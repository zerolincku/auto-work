package httpserver

import (
	"context"
	"flag"
	"fmt"
	"strings"

	"auto-work/internal/db"
	"auto-work/internal/repository"
	"auto-work/internal/service/scheduler"
)

func RunFromArgs(args []string) error {
	fs := flag.NewFlagSet("mcp-http", flag.ContinueOnError)
	dbPath := fs.String("db-path", "", "sqlite db path")
	listenAddr := fs.String("listen", "127.0.0.1:39123", "listen address")
	endpointPath := fs.String("path", "/mcp", "mcp endpoint path")
	runID := fs.String("run-id", "", "default run id (optional, can be passed in URL query run_id)")
	taskID := fs.String("task-id", "", "default task id (optional, can be passed in URL query task_id)")
	if err := fs.Parse(args); err != nil {
		return err
	}

	if strings.TrimSpace(*dbPath) == "" {
		return fmt.Errorf("required flag: --db-path")
	}
	if (strings.TrimSpace(*runID) == "") != (strings.TrimSpace(*taskID) == "") {
		return fmt.Errorf("--run-id and --task-id must be provided together")
	}

	sqlDB, err := db.OpenSQLite(*dbPath)
	if err != nil {
		return err
	}
	defer sqlDB.Close()

	runRepo := repository.NewRunRepository(sqlDB)
	projectRepo := repository.NewProjectRepository(sqlDB)
	taskRepo := repository.NewTaskRepository(sqlDB)
	eventRepo := repository.NewRunEventRepository(sqlDB)
	dispatcher := scheduler.NewDispatcher(sqlDB)

	server := NewServer(runRepo, projectRepo, taskRepo, eventRepo, dispatcher, *runID, *taskID, *endpointPath, nil)
	return server.Serve(context.Background(), *listenAddr)
}
