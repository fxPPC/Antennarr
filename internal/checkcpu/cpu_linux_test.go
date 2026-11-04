//go:build linux

package checkcpu

import (
	"context"
	"os/exec"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkerAndChildrenShareTwoCPUsAndRestore(t *testing.T) {
	var before unix.CPUSet
	if err := unix.SchedGetaffinity(0, &before); err != nil {
		t.Fatal(err)
	}
	ctx, restore, err := Enter(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	var during unix.CPUSet
	if err = unix.SchedGetaffinity(0, &during); err != nil {
		restore()
		t.Fatal(err)
	}
	if during != lastTwo(before) {
		restore()
		t.Fatalf("worker CPUs: %d", during.Count())
	}
	if _, err = exec.LookPath("taskset"); err != nil {
		restore()
		t.Skip("taskset unavailable")
	}
	// Execute from another goroutine to prove affinity survives Go scheduling.
	result := make(chan string, 1)
	go func() {
		b, e := Command(ctx, "sh", "-c", "grep Cpus_allowed_list /proc/self/status").Output()
		if e != nil {
			result <- "error"
			return
		}
		result <- string(b)
	}()
	output := <-result
	restore()
	fields := strings.Fields(output)
	if len(fields) != 2 {
		t.Fatalf("child affinity: %q", output)
	}
	childCPUs := 0
	for _, part := range strings.Split(fields[1], ",") {
		ends := strings.Split(part, "-")
		lo, err := strconv.Atoi(ends[0])
		if err != nil {
			t.Fatal(err)
		}
		hi := lo
		if len(ends) == 2 {
			hi, err = strconv.Atoi(ends[1])
			if err != nil {
				t.Fatal(err)
			}
		}
		for cpu := lo; cpu <= hi; cpu++ {
			if !during.IsSet(cpu) {
				t.Fatalf("child escaped CPU mask: %s", output)
			}
			childCPUs++
		}
	}
	if childCPUs != during.Count() {
		t.Fatalf("child affinity: %s", output)
	}
	var after unix.CPUSet
	if err = unix.SchedGetaffinity(0, &after); err != nil {
		t.Fatal(err)
	}
	if before != after {
		t.Fatal("worker affinity not restored")
	}
}

func TestLastTwoPermittedCPUs(t *testing.T) {
	for _, tc := range []struct{ allowed, want []int }{
		{[]int{0, 1, 2, 3}, []int{2, 3}},
		{[]int{1, 4, 7}, []int{4, 7}},
		{[]int{3}, []int{3}},
	} {
		var mask, expected unix.CPUSet
		for _, id := range tc.allowed {
			mask.Set(id)
		}
		for _, id := range tc.want {
			expected.Set(id)
		}
		if got := lastTwo(mask); got != expected {
			t.Fatalf("allowed %v: incorrect CPU selection", tc.allowed)
		}
	}
}
