package desktopapp

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

func (s *session) startServer(dir string, output io.Writer, _ Server) (func() error, error) {
	server := exec.Command(filepath.Join(dir, "antennarr-server.exe"))
	hideConsole(server)
	server.Env = append(os.Environ(), "ANTENNARR_DESKTOP_CHILD=1")
	server.Stderr = output
	reader, writer, err := os.Pipe()
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	server.Stdin = reader
	statsReader, statsWriter, err := os.Pipe()
	if err != nil {
		writer.Close()
		return nil, err
	}
	defer statsWriter.Close()
	server.Stdout = statsWriter
	if err = server.Start(); err != nil {
		statsReader.Close()
		writer.Close()
		return nil, fmt.Errorf("could not start the Antennarr server: %w", err)
	}
	s.stop = func() { _ = writer.Close() }
	go s.readStats(statsReader)
	return server.Wait, nil
}
