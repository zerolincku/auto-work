package callback

import "testing"

func TestBuildRunScopedURL_AppendsQuery(t *testing.T) {
	t.Parallel()

	got, err := BuildRunScopedURL("http://127.0.0.1:38080/mcp", "run-1", "task-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	want := "http://127.0.0.1:38080/mcp?run_id=run-1&task_id=task-1"
	if got != want {
		t.Fatalf("unexpected url: got=%q want=%q", got, want)
	}
}

func TestBuildRunScopedURL_PreservesExistingQuery(t *testing.T) {
	t.Parallel()

	got, err := BuildRunScopedURL("http://127.0.0.1:38080/mcp?token=abc", "run-1", "task-1")
	if err != nil {
		t.Fatalf("build url: %v", err)
	}
	if got != "http://127.0.0.1:38080/mcp?run_id=run-1&task_id=task-1&token=abc" {
		t.Fatalf("unexpected url: %q", got)
	}
}
