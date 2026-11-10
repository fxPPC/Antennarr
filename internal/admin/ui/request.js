// Deadline covers headers and body. Callers decide whether a failed operation
// is safe to retry; this helper never resubmits mutations.
export async function withDeadline(task, timeoutMS) {
  const controller = new AbortController();
  const timer = setTimeout(() => controller.abort(), timeoutMS);
  try { return await task(controller.signal) }
  catch (error) {
    if (controller.signal.aborted) throw new Error('Request timed out. The server may still be working; check its status before retrying.');
    throw error;
  } finally { clearTimeout(timer) }
}
