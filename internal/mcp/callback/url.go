package callback

import (
	"fmt"
	"net/url"
	"strings"
)

func BuildRunScopedURL(baseURL, runID, taskID string) (string, error) {
	baseURL = strings.TrimSpace(baseURL)
	runID = strings.TrimSpace(runID)
	taskID = strings.TrimSpace(taskID)
	if baseURL == "" {
		return "", fmt.Errorf("mcp http url is empty")
	}
	if runID == "" || taskID == "" {
		return "", fmt.Errorf("run_id/task_id is empty")
	}

	u, err := url.Parse(baseURL)
	if err != nil {
		return "", fmt.Errorf("invalid mcp http url: %w", err)
	}
	q := u.Query()
	q.Set("run_id", runID)
	q.Set("task_id", taskID)
	u.RawQuery = q.Encode()
	return u.String(), nil
}
