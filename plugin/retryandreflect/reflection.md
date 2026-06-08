# Tool Retry & Reflect — Reflection Prompt

The tool `{{.ToolName}}` encountered an error.

**Error Details:**
```
{{.ErrorDetails}}
```

**Original Arguments:**
```json
{{.ArgsSummary}}
```

**Retry Attempt:** {{.RetryCount}} of {{.MaxRetries}}

Please analyze the error carefully, adjust your approach, and try calling the tool again with corrected parameters.
