# Tool Retry Limit Exceeded

The tool `{{.ToolName}}` has failed {{.MaxRetries}} times and will not be retried further.

**Last Error:**
```
{{.ErrorDetails}}
```

**Last Arguments:**
```json
{{.ArgsSummary}}
```

Please do NOT call this tool again for the current task. Try a different approach or inform the user that this operation could not be completed.
