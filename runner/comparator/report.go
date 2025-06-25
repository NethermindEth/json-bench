package comparator

import (
"bytes"
"encoding/json"
"fmt"
"os"
"path/filepath"
"sort"
"strings"
"text/template"
"time"

"github.com/jsonrpc-bench/runner/config"
"github.com/jsonrpc-bench/runner/types"
)

// MethodComparisonResult represents the result of a single method comparison for the report
type MethodComparisonResult struct {
	Method            string                 `json:"method"`
	Params            []interface{}          `json:"params"`
	ParamsDisplay     string                 `json:"params_display"`
	Differences       map[string]interface{} `json:"differences"`
	DifferencesDisplay string                `json:"differences_display"`
	SchemaErrors      map[string][]string    `json:"schema_errors,omitempty"`
	Responses         map[string]string      `json:"responses"`
	Error             error                  `json:"error,omitempty"`
}

// ComparisonSummary represents summary statistics for the comparison
type ComparisonSummary struct {
	TotalMethods       int `json:"total_methods"`
	TotalComparisons   int `json:"total_comparisons"`
	MatchingResponses  int `json:"matching_responses"`
	DifferentResponses int `json:"different_responses"`
	SchemaErrors       int `json:"schema_errors"`
	CallErrors         int `json:"call_errors"`
}

// ReportData represents the data for the HTML report
type ReportData struct {
	Title           string                                  `json:"title"`
	Timestamp       string                                  `json:"timestamp"`
	ComparisonID    string                                  `json:"comparison_id"`
	Configuration   *ComparisonConfig                       `json:"configuration"`
	MethodResults   map[string][]MethodComparisonResult     `json:"method_results"`
	ClientEndpoints []string                                `json:"client_endpoints"`
	ErrorMethods    map[string][]MethodComparisonResult     `json:"error_methods"`
	DiffMethods     map[string][]MethodComparisonResult     `json:"diff_methods"`
	MatchMethods    map[string][]MethodComparisonResult     `json:"match_methods"`
	Summary         ComparisonSummary                       `json:"summary"`
	Scopes          []string                                `json:"scopes"`
	ScopedMethods   map[string]map[string][]MethodComparisonResult `json:"scoped_methods"`
}

// formatJSON formats a JSON object for display
func formatJSON(obj interface{}) (string, error) {
	data, err := json.MarshalIndent(obj, "", "  ")
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// GenerateHTMLReport generates an HTML report from the comparison results
func (c *Comparator) GenerateHTMLReport(outputPath string) error {
	// Convert comparison results to response diffs
	responseDiffs := make([]types.ResponseDiff, len(c.results))
	for i, result := range c.results {
		// Extract client names
		clientNames := make([]string, 0, len(result.Responses))
		for client := range result.Responses {
			clientNames = append(clientNames, client)
		}
		
		// Check if there are differences
		hasDiff := len(result.Differences) > 0
		
		// Create ResponseDiff
		responseDiffs[i] = types.ResponseDiff{
			Method:       result.Method,
			Params:       result.Params,
			Clients:      clientNames,
			ClientNames:  clientNames,
			Responses:    result.Responses,
			Differences:  result.Differences,
			SchemaErrors: result.SchemaErrors,
			HasDiff:      hasDiff,
		}
	}
	
	// Create benchmark result
	clients := make([]config.Client, len(c.config.Clients))
	for i, client := range c.config.Clients {
		clients[i] = config.Client{
			Name: client.Name,
			URL:  client.URL,
		}
	}
	
	cfg := &config.Config{
		Clients: clients,
	}
	
	benchmarkResult := &types.BenchmarkResult{
		Config: cfg,
		ResponseDiff: map[string]interface{}{
			"diffs": responseDiffs,
		},
		Timestamp: time.Now().Format(time.RFC3339),
	}

	// Create output directory if it doesn't exist
outputDir := filepath.Dir(outputPath)
if err := os.MkdirAll(outputDir, 0755); err != nil {
return fmt.Errorf("failed to create output directory: %w", err)
}

// Generate HTML report using the template
tmpl, err := template.New("report").Parse(htmlReportTemplate)
if err != nil {
return fmt.Errorf("failed to parse HTML template: %w", err)
}

var buf bytes.Buffer
if err := tmpl.Execute(&buf, reportData(benchmarkResult, responseDiffs, outputPath)); err != nil {
return fmt.Errorf("failed to execute template: %w", err)
}

// Write to file
if err := os.WriteFile(outputPath, buf.Bytes(), 0644); err != nil {
return fmt.Errorf("failed to write HTML report: %w", err)
}

return nil
}

// reportData creates the report data from benchmark result and response diffs
func reportData(result *types.BenchmarkResult, diffs []types.ResponseDiff, outputPath string) ReportData {
// Create report data
reportData := ReportData{
Title:           "JSON-RPC Response Comparison Report",
Timestamp:       time.Now().Format(time.RFC1123),
ComparisonID:    time.Now().Format("20060102-150405"),
Configuration:   &ComparisonConfig{}, // Placeholder
MethodResults:   make(map[string][]MethodComparisonResult),
ClientEndpoints: make([]string, 0),
ErrorMethods:    make(map[string][]MethodComparisonResult),
DiffMethods:     make(map[string][]MethodComparisonResult),
MatchMethods:    make(map[string][]MethodComparisonResult),
ScopedMethods:   make(map[string]map[string][]MethodComparisonResult),
Scopes:          make([]string, 0),
}

// Add client endpoints
if cfg, ok := result.Config.(*config.Config); ok && cfg != nil {
for _, client := range cfg.Clients {
reportData.ClientEndpoints = append(reportData.ClientEndpoints, fmt.Sprintf("%s: %s", client.Name, client.URL))
}
}
sort.Strings(reportData.ClientEndpoints)

// Process each response diff into method comparison results
for _, diff := range diffs {
// Convert raw comparison result to report format
paramsJSON, _ := json.Marshal(diff.Params)

// Format responses for display
formattedResponses := make(map[string]string)
for client, resp := range diff.Responses {
formatted, _ := formatJSON(resp)
formattedResponses[client] = formatted
}

// Format differences for display
diffDisplay := ""
if len(diff.Differences) > 0 {
diffJSON, _ := formatJSON(diff.Differences)
diffDisplay = diffJSON
}

methodResult := MethodComparisonResult{
Method:            diff.Method,
Params:            diff.Params,
ParamsDisplay:     string(paramsJSON),
Differences:       diff.Differences,
DifferencesDisplay: diffDisplay,
SchemaErrors:      diff.SchemaErrors,
Responses:         formattedResponses,
}

// Add to method results map
if _, exists := reportData.MethodResults[diff.Method]; !exists {
reportData.MethodResults[diff.Method] = make([]MethodComparisonResult, 0)
}
reportData.MethodResults[diff.Method] = append(reportData.MethodResults[diff.Method], methodResult)

// Add to grouped results based on status
hasError := false
hasDiff := false

// Check for errors
if methodResult.Error != nil {
hasError = true
}

// Check for schema errors
if len(methodResult.SchemaErrors) > 0 {
hasError = true
}

// Check for differences
if len(methodResult.Differences) > 0 {
hasDiff = true
}

// Add to appropriate group
if hasError {
if _, exists := reportData.ErrorMethods[diff.Method]; !exists {
reportData.ErrorMethods[diff.Method] = make([]MethodComparisonResult, 0)
}
reportData.ErrorMethods[diff.Method] = append(reportData.ErrorMethods[diff.Method], methodResult)
} else if hasDiff {
if _, exists := reportData.DiffMethods[diff.Method]; !exists {
reportData.DiffMethods[diff.Method] = make([]MethodComparisonResult, 0)
}
reportData.DiffMethods[diff.Method] = append(reportData.DiffMethods[diff.Method], methodResult)
} else {
if _, exists := reportData.MatchMethods[diff.Method]; !exists {
reportData.MatchMethods[diff.Method] = make([]MethodComparisonResult, 0)
}
reportData.MatchMethods[diff.Method] = append(reportData.MatchMethods[diff.Method], methodResult)
}

// Extract method scope (namespace)
scope := "other"
parts := strings.Split(diff.Method, "_")
if len(parts) > 1 {
scope = parts[0] + "_"
}

// Add to scoped methods
if _, exists := reportData.ScopedMethods[scope]; !exists {
reportData.ScopedMethods[scope] = make(map[string][]MethodComparisonResult)
reportData.Scopes = append(reportData.Scopes, scope)
}

if _, exists := reportData.ScopedMethods[scope][diff.Method]; !exists {
reportData.ScopedMethods[scope][diff.Method] = make([]MethodComparisonResult, 0)
}

reportData.ScopedMethods[scope][diff.Method] = append(reportData.ScopedMethods[scope][diff.Method], methodResult)
}

// Calculate summary statistics
summary := ComparisonSummary{
TotalMethods:     len(reportData.MethodResults),
TotalComparisons: len(diffs),
MatchingResponses: 0,
DifferentResponses: 0,
SchemaErrors:     0,
CallErrors:       0,
}

for _, diff := range diffs {
if len(diff.SchemaErrors) > 0 {
summary.SchemaErrors++
}

if len(diff.Differences) > 0 {
summary.DifferentResponses++
} else {
summary.MatchingResponses++
}
}
reportData.Summary = summary

return reportData
}

// formatJSONForDisplay formats JSON data for display in the HTML report
// This is kept for backward compatibility but formatJSON should be used instead
func formatJSONForDisplay(data interface{}) (string, error) {
return formatJSON(data)
}

// htmlReportTemplate is the HTML template for the comparison report
const htmlReportTemplate = `<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>{{.Title}}</title>
    <style>
        body {
            font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
            line-height: 1.6;
            color: #333;
            max-width: 1200px;
            margin: 0 auto;
            padding: 20px;
            background-color: #f8f9fa;
        }
        .header {
            background: linear-gradient(135deg, #667eea 0%, #764ba2 100%);
            color: white;
            padding: 30px;
            border-radius: 10px;
            margin-bottom: 30px;
            text-align: center;
        }
        .header h1 {
            margin: 0 0 10px 0;
            font-size: 2.5em;
        }
        .header p {
            margin: 5px 0;
            opacity: 0.9;
        }
        .summary {
            display: grid;
            grid-template-columns: repeat(auto-fit, minmax(200px, 1fr));
            gap: 20px;
            margin-bottom: 30px;
        }
        .summary-card {
            background: white;
            padding: 20px;
            border-radius: 8px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            text-align: center;
        }
        .summary-card h3 {
            margin: 0 0 10px 0;
            color: #666;
            font-size: 0.9em;
            text-transform: uppercase;
            letter-spacing: 1px;
        }
        .summary-card .number {
            font-size: 2em;
            font-weight: bold;
            color: #333;
        }
        .config {
            margin-bottom: 20px;
        }
        .method-section {
            margin-bottom: 30px;
            border: 1px solid #eee;
            border-radius: 5px;
            overflow: hidden;
        }
        .method-header {
            background-color: #f8f9fa;
            padding: 10px 15px;
            border-bottom: 1px solid #eee;
            cursor: pointer;
        }
        .method-content {
            padding: 15px;
            display: none;
        }
        .method-content.active {
            display: block;
        }
        .tabs {
            display: flex;
            background-color: white;
            border-radius: 8px;
            overflow: hidden;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
            margin-bottom: 20px;
        }
        .tab-button {
            flex: 1;
            padding: 15px 20px;
            background: #f8f9fa;
            border: none;
            cursor: pointer;
            font-size: 14px;
            font-weight: 500;
            transition: all 0.3s ease;
            border-right: 1px solid #dee2e6;
        }
        .tab-button:last-child {
            border-right: none;
        }
        .tab-button:hover {
            background: #e9ecef;
        }
        .tab-button.active {
            background: #007bff;
            color: white;
        }
        .tab-content {
            display: none;
            background: white;
            border-radius: 8px;
            padding: 20px;
            box-shadow: 0 2px 10px rgba(0,0,0,0.1);
        }
        .tab-content.active {
            display: block;
        }
        .diff-table {
            width: 100%;
            border-collapse: collapse;
            margin-top: 10px;
        }
        .diff-table th, .diff-table td {
            padding: 8px;
            text-align: left;
            border: 1px solid #ddd;
        }
        .diff-table th {
            background-color: #f8f9fa;
        }
        pre {
            background-color: #f8f9fa;
            padding: 10px;
            border-radius: 5px;
            overflow-x: auto;
            font-size: 13px;
            white-space: pre-wrap;
            word-break: break-word;
            max-height: 300px;
            overflow-y: auto;
            border: 1px solid #e9ecef;
        }
        .code-container {
            position: relative;
            margin: 10px 0;
        }
        .code-header {
            display: flex;
            justify-content: space-between;
            align-items: center;
            background-color: #f1f3f4;
            padding: 8px 12px;
            border-radius: 5px 5px 0 0;
            border: 1px solid #e9ecef;
            border-bottom: none;
        }
        .code-title {
            font-weight: 500;
            font-size: 14px;
            color: #333;
        }
        .code-content {
            margin: 0;
            border-radius: 0 0 5px 5px;
        }
        .copy-btn {
            background-color: #4CAF50;
            color: white;
            border: none;
            padding: 4px 8px;
            text-align: center;
            text-decoration: none;
            display: inline-block;
            font-size: 12px;
            margin: 2px 2px;
            cursor: pointer;
            border-radius: 3px;
        }
        
        .copy-btn:hover {
            background-color: #45a049;
        }
        
        .copy-btn:active {
            background-color: #3e8e41;
        }
        
        .copy-success {
            background-color: #2196F3;
        }
        
        .copy-error {
            background-color: #f44336;
        }
        
        /* Status-specific styling */
        .error-section .method-header {
            background-color: #ffebee;
            border-left: 4px solid #f44336;
        }
        
        .diff-section .method-header {
            background-color: #fff8e1;
            border-left: 4px solid #ffc107;
        }
        
        .match-section .method-header {
            background-color: #e8f5e9;
            border-left: 4px solid #4caf50;
        }
        
        /* Scope styles */
        .scope-header {
            background-color: #e3f2fd;
            padding: 15px;
            margin: 20px 0 10px 0;
            border-radius: 5px;
            border-left: 4px solid #2196f3;
        }
        .scope-header h3 {
            margin: 0;
            color: #1976d2;
        }
        .client-list {
            list-style-type: none;
            padding: 0;
        }
        .client-list li {
            margin-bottom: 5px;
            padding: 5px;
            background-color: #f8f9fa;
            border-radius: 3px;
        }
    </style>
</head>
<body>
    <div class="header">
        <h1>{{.Title}}</h1>
        <p>Generated: {{.Timestamp}}</p>
        <p>Comparison ID: {{.ComparisonID}}</p>
    </div>

    <div class="summary">
        <div class="summary-card">
            <h3>Total Methods</h3>
            <div class="number">{{.Summary.TotalMethods}}</div>
        </div>
        <div class="summary-card">
            <h3>Total Comparisons</h3>
            <div class="number">{{.Summary.TotalComparisons}}</div>
        </div>
        <div class="summary-card">
            <h3>Matching Responses</h3>
            <div class="number">{{.Summary.MatchingResponses}}</div>
        </div>
        <div class="summary-card">
            <h3>Different Responses</h3>
            <div class="number">{{.Summary.DifferentResponses}}</div>
        </div>
        <div class="summary-card">
            <h3>Schema Errors</h3>
            <div class="number">{{.Summary.SchemaErrors}}</div>
        </div>
    </div>

    <div class="config">
        <h2>Client Endpoints</h2>
        <ul class="client-list">
            {{range .ClientEndpoints}}
            <li>{{.}}</li>
            {{end}}
        </ul>
    </div>

    <div class="tabs">
        <button class="tab-button active" onclick="openTab('errors')">Errors ({{len .ErrorMethods}})</button>
        <button class="tab-button" onclick="openTab('differences')">Differences ({{len .DiffMethods}})</button>
        <button class="tab-button" onclick="openTab('matches')">Matches ({{len .MatchMethods}})</button>
        <button class="tab-button" onclick="openTab('scopes')">By Scope</button>
        <button class="tab-button" onclick="openTab('all')">All Methods</button>
    </div>

    <--input.txs=./Evm.Test/testdata/1/txs.json
--input.env=./Evm.Test/testdata/1/env.json
--output.alloc=stdout
--output.result=stdout
--output.body=stdout Errors Tab -->
    <div id="errors" class="tab-content active">
        <h2>Methods with Errors</h2>
        {{range $method, $results := .ErrorMethods}}
        <div class="method-section error-section">
            <div class="method-header" onclick="toggleSection('error-{{$method}}')">
                <h3>{{$method}} ({{len $results}} comparison{{if gt (len $results) 1}}s{{end}})</h3>
            </div>
            <div id="error-{{$method}}" class="method-content">
                {{range $result := $results}}
                <div>
                    <h4>Parameters:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Request Parameters</div>
                            <div>
                                <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.ParamsDisplay}}')">Copy Params</button>
                                <button class="copy-btn" onclick="copyRequest('{{$result.Method}}', '{{$result.ParamsDisplay}}')">Copy Request</button>
                            </div>
                        </div>
                        <pre class="code-content">{{$result.ParamsDisplay}}</pre>
                    </div>
                </div>
                
                {{if $result.SchemaErrors}}
                <div>
                    <h4>Schema Errors:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Errors</th>
                        </tr>
                        {{range $client, $errors := $result.SchemaErrors}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Schema Errors</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{range $errors}}{{.}}\n{{end}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{range $errors}}{{.}}{{end}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
                
                <div>
                    <h4>Responses:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Response</th>
                        </tr>
                        {{range $client, $response := $result.Responses}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Response</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{$response}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{$response}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>

    <--input.txs=./Evm.Test/testdata/1/txs.json
--input.env=./Evm.Test/testdata/1/env.json
--output.alloc=stdout
--output.result=stdout
--output.body=stdout Differences Tab -->
    <div id="differences" class="tab-content">
        <h2>Methods with Different Responses</h2>
        {{range $method, $results := .DiffMethods}}
        <div class="method-section diff-section">
            <div class="method-header" onclick="toggleSection('diff-{{$method}}')">
                <h3>{{$method}} ({{len $results}} comparison{{if gt (len $results) 1}}s{{end}})</h3>
            </div>
            <div id="diff-{{$method}}" class="method-content">
                {{range $result := $results}}
                <div>
                    <h4>Parameters:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Request Parameters</div>
                            <div>
                                <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.ParamsDisplay}}')">Copy Params</button>
                                <button class="copy-btn" onclick="copyRequest('{{$result.Method}}', '{{$result.ParamsDisplay}}')">Copy Request</button>
                            </div>
                        </div>
                        <pre class="code-content">{{$result.ParamsDisplay}}</pre>
                    </div>
                </div>
                
                {{if $result.Differences}}
                <div>
                    <h4>Response Differences:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Differences</div>
                            <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.DifferencesDisplay}}')">Copy</button>
                        </div>
                        <pre class="code-content">{{$result.DifferencesDisplay}}</pre>
                    </div>
                </div>
                {{end}}
                
                <div>
                    <h4>Responses:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Response</th>
                        </tr>
                        {{range $client, $response := $result.Responses}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Response</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{$response}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{$response}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>

    <--input.txs=./Evm.Test/testdata/1/txs.json
--input.env=./Evm.Test/testdata/1/env.json
--output.alloc=stdout
--output.result=stdout
--output.body=stdout Matches Tab -->
    <div id="matches" class="tab-content">
        <h2>Methods with Matching Responses</h2>
        {{range $method, $results := .MatchMethods}}
        <div class="method-section match-section">
            <div class="method-header" onclick="toggleSection('match-{{$method}}')">
                <h3>{{$method}} ({{len $results}} comparison{{if gt (len $results) 1}}s{{end}})</h3>
            </div>
            <div id="match-{{$method}}" class="method-content">
                {{range $result := $results}}
                <div>
                    <h4>Parameters:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Request Parameters</div>
                            <div>
                                <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.ParamsDisplay}}')">Copy Params</button>
                                <button class="copy-btn" onclick="copyRequest('{{$result.Method}}', '{{$result.ParamsDisplay}}')">Copy Request</button>
                            </div>
                        </div>
                        <pre class="code-content">{{$result.ParamsDisplay}}</pre>
                    </div>
                </div>
                
                <div>
                    <h4>Responses:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Response</th>
                        </tr>
                        {{range $client, $response := $result.Responses}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Response</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{$response}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{$response}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>

    <--input.txs=./Evm.Test/testdata/1/txs.json
--input.env=./Evm.Test/testdata/1/env.json
--output.alloc=stdout
--output.result=stdout
--output.body=stdout Scopes Tab -->
    <div id="scopes" class="tab-content">
        <h2>Methods by Scope</h2>
        {{range $scope := .Scopes}}
        <div class="scope-header">
            <h3>{{$scope}} Methods</h3>
        </div>
        {{range $method, $results := index $.ScopedMethods $scope}}
        <div class="method-section">
            <div class="method-header" onclick="toggleSection('scope-{{$scope}}-{{$method}}')">
                <h3>{{$method}} ({{len $results}} comparison{{if gt (len $results) 1}}s{{end}})</h3>
            </div>
            <div id="scope-{{$scope}}-{{$method}}" class="method-content">
                {{range $result := $results}}
                <div>
                    <h4>Parameters:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Request Parameters</div>
                            <div>
                                <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.ParamsDisplay}}')">Copy Params</button>
                                <button class="copy-btn" onclick="copyRequest('{{$result.Method}}', '{{$result.ParamsDisplay}}')">Copy Request</button>
                            </div>
                        </div>
                        <pre class="code-content">{{$result.ParamsDisplay}}</pre>
                    </div>
                </div>
                
                {{if $result.SchemaErrors}}
                <div>
                    <h4>Schema Errors:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Errors</th>
                        </tr>
                        {{range $client, $errors := $result.SchemaErrors}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Schema Errors</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{range $errors}}{{.}}\n{{end}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{range $errors}}{{.}}{{end}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
                
                {{if $result.Differences}}
                <div>
                    <h4>Response Differences:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Differences</div>
                            <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.DifferencesDisplay}}')">Copy</button>
                        </div>
                        <pre class="code-content">{{$result.DifferencesDisplay}}</pre>
                    </div>
                </div>
                {{end}}
                
                <div>
                    <h4>Responses:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Response</th>
                        </tr>
                        {{range $client, $response := $result.Responses}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Response</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{$response}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{$response}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
        {{end}}
    </div>

    <--input.txs=./Evm.Test/testdata/1/txs.json
--input.env=./Evm.Test/testdata/1/env.json
--output.alloc=stdout
--output.result=stdout
--output.body=stdout All Methods Tab -->
    <div id="all" class="tab-content">
        <h2>All Methods</h2>
        {{range $method, $results := .MethodResults}}
        <div class="method-section">
            <div class="method-header" onclick="toggleSection('all-{{$method}}')">
                <h3>{{$method}} ({{len $results}} comparison{{if gt (len $results) 1}}s{{end}})</h3>
            </div>
            <div id="all-{{$method}}" class="method-content">
                {{range $result := $results}}
                <div>
                    <h4>Parameters:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Request Parameters</div>
                            <div>
                                <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.ParamsDisplay}}')">Copy Params</button>
                                <button class="copy-btn" onclick="copyRequest('{{$result.Method}}', '{{$result.ParamsDisplay}}')">Copy Request</button>
                            </div>
                        </div>
                        <pre class="code-content">{{$result.ParamsDisplay}}</pre>
                    </div>
                </div>
                
                {{if $result.SchemaErrors}}
                <div>
                    <h4>Schema Errors:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Errors</th>
                        </tr>
                        {{range $client, $errors := $result.SchemaErrors}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Schema Errors</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{range $errors}}{{.}}\n{{end}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{range $errors}}{{.}}{{end}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
                
                {{if $result.Differences}}
                <div>
                    <h4>Response Differences:</h4>
                    <div class="code-container">
                        <div class="code-header">
                            <div class="code-title">Differences</div>
                            <button class="copy-btn" onclick="copyToClipboard(this, '{{$result.DifferencesDisplay}}')">Copy</button>
                        </div>
                        <pre class="code-content">{{$result.DifferencesDisplay}}</pre>
                    </div>
                </div>
                {{end}}
                
                <div>
                    <h4>Responses:</h4>
                    <table class="diff-table">
                        <tr>
                            <th>Client</th>
                            <th>Response</th>
                        </tr>
                        {{range $client, $response := $result.Responses}}
                        <tr>
                            <td>{{$client}}</td>
                            <td>
                                <div class="code-container">
                                    <div class="code-header">
                                        <div class="code-title">Response</div>
                                        <button class="copy-btn" onclick="copyToClipboard(this, '{{$response}}')">Copy</button>
                                    </div>
                                    <pre class="code-content">{{$response}}</pre>
                                </div>
                            </td>
                        </tr>
                        {{end}}
                    </table>
                </div>
                {{end}}
            </div>
        </div>
        {{end}}
    </div>

    <script>
        function toggleSection(id) {
            const content = document.getElementById(id);
            content.classList.toggle('active');
        }
        
        function openTab(tabName) {
            // Hide all tab contents
            const tabContents = document.getElementsByClassName('tab-content');
            for (let i = 0; i < tabContents.length; i++) {
                tabContents[i].classList.remove('active');
            }
            
            // Deactivate all tab buttons
            const tabButtons = document.getElementsByClassName('tab-button');
            for (let i = 0; i < tabButtons.length; i++) {
                tabButtons[i].classList.remove('active');
            }
            
            // Show the selected tab content
            document.getElementById(tabName).classList.add('active');
            
            // Activate the clicked tab button
            const activeButton = document.querySelector(".tab-button[onclick=\"openTab('"+tabName+"')\"]");
            if (activeButton) {
                activeButton.classList.add('active');
            }
        }
        
        function copyToClipboard(button, text) {
            navigator.clipboard.writeText(text).then(function() {
                // Success feedback
                const originalText = button.textContent;
                button.textContent = 'Copied!';
                button.classList.add('copy-success');
                
                // Reset button after 2 seconds
                setTimeout(function() {
                    button.textContent = originalText;
                    button.classList.remove('copy-success');
                }, 2000);
            }, function() {
                // Error feedback
                button.textContent = 'Failed!';
                button.classList.add('copy-error');
                
                // Reset button after 2 seconds
                setTimeout(function() {
                    button.textContent = 'Copy';
                    button.classList.remove('copy-error');
                }, 2000);
            });
        }
        
        function copyRequest(method, params) {
            const request = {
                jsonrpc: '2.0',
                id: 1,
                method: method,
                params: JSON.parse(params)
            };
            
            copyToClipboard(event.target, JSON.stringify(request, null, 2));
        }
    </script>
</body>
</html>
`
