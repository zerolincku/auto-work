package report

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
)

const (
	toolReportResult  = "auto-work.report_result"
	toolCreateTasks   = "auto-work.create_tasks"
	toolListPending   = "auto-work.list_pending_tasks"
	toolListHistory   = "auto-work.list_history_tasks"
	toolGetTaskDetail = "auto-work.get_task_detail"
)

func ToolListResult() map[string]any {
	return map[string]any{
		"tools": []map[string]any{
			{
				"name":        toolReportResult,
				"description": "上报任务执行结果并回写 run/task 终态",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"status": map[string]any{
							"type":        "string",
							"description": "success | failed | blocked",
						},
						"summary": map[string]any{
							"type":        "string",
							"description": "简短总结",
						},
						"details": map[string]any{
							"type":        "string",
							"description": "详细说明",
						},
						"exit_code": map[string]any{
							"type":        "number",
							"description": "可选退出码",
						},
						"idempotency_key": map[string]any{
							"type":        "string",
							"description": "幂等键（可选）",
						},
					},
					"required": []string{"status", "summary"},
				},
			},
			{
				"name":        toolCreateTasks,
				"description": "批量创建后续任务（归属当前项目，优先级自动追加到队尾）",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"items": map[string]any{
							"type": "array",
							"items": map[string]any{
								"type": "object",
								"properties": map[string]any{
									"title": map[string]any{
										"type": "string",
									},
									"description": map[string]any{
										"type": "string",
									},
									"depends_on": map[string]any{
										"type":  "array",
										"items": map[string]any{"type": "string"},
									},
									"provider": map[string]any{
										"type": "string",
									},
								},
								"required": []string{"title", "description"},
							},
							"minItems": 1,
							"maxItems": 50,
						},
						"project_id": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失时，优先按项目ID匹配",
						},
						"project_name": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id 时，按项目名称匹配",
						},
						"project_path": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id/project_name 时，按项目绝对路径匹配",
						},
					},
					"required": []string{"items"},
				},
			},
			{
				"name":        toolListPending,
				"description": "查询当前项目未办任务（pending）",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{
							"type":        "number",
							"description": "返回条数，默认20，最大100",
						},
						"project_id": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失时，优先按项目ID匹配",
						},
						"project_name": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id 时，按项目名称匹配",
						},
						"project_path": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id/project_name 时，按项目绝对路径匹配",
						},
					},
				},
			},
			{
				"name":        toolListHistory,
				"description": "查询当前项目历史任务（done/failed/blocked）",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"limit": map[string]any{
							"type":        "number",
							"description": "返回条数，默认20，最大100",
						},
						"project_id": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失时，优先按项目ID匹配",
						},
						"project_name": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id 时，按项目名称匹配",
						},
						"project_path": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id/project_name 时，按项目绝对路径匹配",
						},
					},
				},
			},
			{
				"name":        toolGetTaskDetail,
				"description": "查询任务详情（含最近运行记录）",
				"inputSchema": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"task_id": map[string]any{
							"type":        "string",
							"description": "任务ID",
						},
						"project_id": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失时，优先按项目ID匹配",
						},
						"project_name": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id 时，按项目名称匹配",
						},
						"project_path": map[string]any{
							"type":        "string",
							"description": "可选：当 run/task 上下文缺失且未提供 project_id/project_name 时，按项目绝对路径匹配",
						},
					},
					"required": []string{"task_id"},
				},
			},
		},
	}
}

func HandleToolCall(ctx context.Context, reporter *Service, raw json.RawMessage) (string, error) {
	var call toolCallParams
	if err := json.Unmarshal(raw, &call); err != nil {
		return "", err
	}
	name := normalizeToolName(call.Name)
	if name != toolReportResult {
		if name == toolCreateTasks {
			var in CreateTasksInput
			b, err := json.Marshal(call.Arguments)
			if err != nil {
				return "", err
			}
			if err := json.Unmarshal(b, &in); err != nil {
				return "", err
			}
			items, err := reporter.CreateTasks(ctx, in)
			if err != nil {
				return "", err
			}
			out := map[string]any{
				"created_count": len(items),
				"items":         items,
			}
			res, err := json.Marshal(out)
			if err != nil {
				return "", err
			}
			return string(res), nil
		}
		if name == toolListPending {
			limit := intArg(call.Arguments, "limit", 20)
			items, err := reporter.ListPendingTasks(ctx, limit, selectorFromArgs(call.Arguments))
			if err != nil {
				return "", err
			}
			res, err := json.Marshal(map[string]any{
				"count": len(items),
				"items": items,
			})
			if err != nil {
				return "", err
			}
			return string(res), nil
		}
		if name == toolListHistory {
			limit := intArg(call.Arguments, "limit", 20)
			items, err := reporter.ListHistoryTasks(ctx, limit, selectorFromArgs(call.Arguments))
			if err != nil {
				return "", err
			}
			res, err := json.Marshal(map[string]any{
				"count": len(items),
				"items": items,
			})
			if err != nil {
				return "", err
			}
			return string(res), nil
		}
		if name == toolGetTaskDetail {
			taskID, _ := call.Arguments["task_id"].(string)
			detail, err := reporter.GetTaskDetail(ctx, taskID, selectorFromArgs(call.Arguments))
			if err != nil {
				return "", err
			}
			res, err := json.Marshal(detail)
			if err != nil {
				return "", err
			}
			return string(res), nil
		}
		return "", fmt.Errorf("unsupported tool: %s", call.Name)
	}

	var in ResultInput
	b, err := json.Marshal(call.Arguments)
	if err != nil {
		return "", err
	}
	if err := json.Unmarshal(b, &in); err != nil {
		return "", err
	}
	if strings.TrimSpace(in.Details) == "" {
		in.Details = "no details provided"
	}
	return reporter.ReportResult(ctx, in)
}

func normalizeToolName(name string) string {
	return strings.TrimSpace(name)
}

type toolCallParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func intArg(args map[string]any, key string, fallback int) int {
	raw, ok := args[key]
	if !ok {
		return fallback
	}
	switch v := raw.(type) {
	case float64:
		return int(v)
	case int:
		return v
	default:
		return fallback
	}
}

func selectorFromArgs(args map[string]any) ProjectSelectorInput {
	return ProjectSelectorInput{
		ProjectID:   stringArg(args, "project_id"),
		ProjectName: stringArg(args, "project_name"),
		ProjectPath: stringArg(args, "project_path"),
	}
}

func stringArg(args map[string]any, key string) string {
	raw, ok := args[key]
	if !ok {
		return ""
	}
	v, _ := raw.(string)
	return strings.TrimSpace(v)
}
