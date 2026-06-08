package security

// Standard returns a security policy suitable for developer agents.
// Allows shell commands except destructive ones, restricts file access
// to the project directory, and allows outbound network requests.
//
// Blocked commands: rm -rf, sudo, chmod 777, mkfs, dd, :(){ :|:& };:,
// curl (upload), wget (to prevent exfiltration).
func Standard(projectDir string) Policy {
	return Policy{
		AllowedPaths: []string{projectDir},
		DeniedCommands: []string{
			"rm -rf /",
			"rm -rf ~",
			"sudo",
			"chmod 777",
			"mkfs",
			"dd if=",
			":(){ :|:& };:",
			"> /dev/sd",
			"curl -X POST",
			"curl --upload",
			"wget --post",
		},
		MaxFileSize: 10 * 1024 * 1024, // 10MB
		Network: NetworkPolicy{
			AllowOutbound: true,
			DeniedDomains: []string{
				"localhost",
				"127.0.0.1",
				"0.0.0.0",
				"metadata.google.internal",
				"169.254.169.254", // AWS/GCP metadata
			},
		},
	}
}

// Strict returns a locked-down policy. Every command requires HITL approval,
// file access is restricted, and no outbound network requests are allowed.
func Strict(projectDir string) Policy {
	return Policy{
		AllowedPaths: []string{projectDir},
		DeniedCommands: []string{
			"rm -rf",
			"sudo",
			"chmod",
			"chown",
			"mkfs",
			"dd ",
			"curl",
			"wget",
			"nc ",
			"ncat",
			"ssh",
			"scp",
		},
		MaxFileSize: 1024 * 1024, // 1MB
		RequireConfirmation: []string{
			"run_command",
			"execute_command",
			"write_file",
		},
		Network: NetworkPolicy{
			AllowOutbound: false,
		},
	}
}

// Permissive returns a minimal policy with no restrictions.
// Use only in trusted/sandboxed environments (containers, CI).
func Permissive() Policy {
	return Policy{
		Network: NetworkPolicy{
			AllowOutbound: true,
		},
	}
}
