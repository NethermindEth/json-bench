package config

import (
	"fmt"
	"os"
	"regexp"
	"strings"
)

var (
	// defaultValueRegex matches the default value pattern: VAR:-default
	defaultValueRegex = regexp.MustCompile(`^(.+?):-(.+)$`)
	// requiredValueRegex matches the required pattern: VAR:?error message (error message is optional)
	requiredValueRegex = regexp.MustCompile(`^(.+?):\?(.*)$`)
)

// SubstituteEnvVars replaces environment variable references in YAML content
// Supports:
//   - ${VAR_NAME} - basic substitution
//   - ${VAR:-default} - use default if VAR is empty/unset
//   - ${VAR:?error message} - error if VAR is empty/unset
//   - $${VAR} - escape sequence, results in literal ${VAR}
//
// Returns the substituted content and any error encountered.
func SubstituteEnvVars(content string) (string, error) {
	var substitutionError error
	var result strings.Builder
	result.Grow(len(content))

	i := 0
	for i < len(content) {
		// Look for ${ or $${ (escaped)
		if i < len(content)-1 && content[i] == '$' {
			if i < len(content)-2 && content[i+1] == '$' && content[i+2] == '{' {
				// This is an escaped sequence: $${VAR} -> ${VAR}
				// Output literal ${ and skip past $${, then find the closing }
				result.WriteString("${")
				i += 3 // Skip past $${

				// Find the closing } and skip past it
				closeIdx := strings.Index(content[i:], "}")
				if closeIdx != -1 {
					// Output the variable name and closing brace as-is
					result.WriteString(content[i : i+closeIdx+1])
					i += closeIdx + 1
				} else {
					// No closing brace, output rest as-is
					result.WriteString(content[i:])
					break
				}
				continue
			}

			if content[i+1] == '{' {
				// Regular ${VAR} substitution
				// Find the closing }
				closeIdx := strings.Index(content[i+2:], "}")
				if closeIdx == -1 {
					// No closing brace, output as-is
					result.WriteByte(content[i])
					i++
					continue
				}

				closeIdx += i + 2 // Adjust to absolute position
				varName := content[i+2 : closeIdx]

				// Check for required variable pattern: ${VAR:?error}
				if requiredValueRegex.MatchString(varName) {
					matches := requiredValueRegex.FindStringSubmatch(varName)
					if len(matches) == 3 {
						actualVarName := strings.TrimSpace(matches[1])
						errorMsg := strings.TrimSpace(matches[2])
						value := os.Getenv(actualVarName)
						if value == "" {
							if errorMsg == "" {
								errorMsg = fmt.Sprintf("required environment variable %s is not set", actualVarName)
							}
							substitutionError = fmt.Errorf("%s", errorMsg)
							// Continue processing but mark error
						} else {
							result.WriteString(value)
						}
						i = closeIdx + 1
						continue
					}
				}

				// Check for default value pattern: ${VAR:-default}
				if defaultValueRegex.MatchString(varName) {
					matches := defaultValueRegex.FindStringSubmatch(varName)
					if len(matches) == 3 {
						actualVarName := strings.TrimSpace(matches[1])
						defaultValue := strings.TrimSpace(matches[2])
						value := os.Getenv(actualVarName)
						if value == "" {
							result.WriteString(defaultValue)
						} else {
							result.WriteString(value)
						}
						i = closeIdx + 1
						continue
					}
				}

				// Basic substitution: ${VAR_NAME}
				value := os.Getenv(varName)
				result.WriteString(value)
				i = closeIdx + 1
				continue
			}
		}

		// Regular character, output as-is
		result.WriteByte(content[i])
		i++
	}

	if substitutionError != nil {
		return result.String(), substitutionError
	}

	return result.String(), nil
}
