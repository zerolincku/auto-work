UPDATE global_settings
SET system_prompt = REPLACE(
  system_prompt,
  '10) Before calling auto-work.report_result, you must run git commit for this task: run git status --porcelain; if there are changes then git add -A and git commit -m "任务 {{task_id}}：{{task_title}}"; if no changes then create an empty commit git commit --allow-empty -m "任务 {{task_id}}：{{task_title}}（空提交）".',
  '10) Before calling auto-work.report_result, you must run git commit for this task: run git status --porcelain; if there are changes then git add -A, review staged changes (git diff --cached --name-only and git diff --cached), and write a concise Chinese commit message based on the actual code/file changes (do not include task_id/run_id or fixed prefixes), then run git commit -m "<中文提交信息>"; if no changes then create an empty commit git commit --allow-empty -m "空提交：本次任务无需修改代码".'
)
WHERE INSTR(
  system_prompt,
  '10) Before calling auto-work.report_result, you must run git commit for this task: run git status --porcelain; if there are changes then git add -A and git commit -m "任务 {{task_id}}：{{task_title}}"; if no changes then create an empty commit git commit --allow-empty -m "任务 {{task_id}}：{{task_title}}（空提交）".'
) > 0;
