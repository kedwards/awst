package sessions

import (
	"bytes"
	"encoding/json"
)

// cimProcess is one row of
// `Get-CimInstance Win32_Process | Select-Object ProcessId,CommandLine`.
type cimProcess struct {
	ProcessId   int    `json:"ProcessId"`
	CommandLine string `json:"CommandLine"`
}

// parseCimProcesses turns PowerShell ConvertTo-Json output into Sessions.
// ConvertTo-Json emits a bare object for a single match and an array for
// many (and "null"/empty for none) — all handled. split turns a Windows
// command line into argv (CommandLineToArgvW in production; injected in
// tests so this stays OS-independent and unit-testable on any platform).
func parseCimProcesses(data []byte, split func(string) []string) ([]Session, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return nil, nil
	}
	var rows []cimProcess
	if data[0] == '[' {
		if err := json.Unmarshal(data, &rows); err != nil {
			return nil, err
		}
	} else {
		var one cimProcess
		if err := json.Unmarshal(data, &one); err != nil {
			return nil, err
		}
		rows = []cimProcess{one}
	}

	var out []Session
	for _, r := range rows {
		if r.CommandLine == "" {
			continue // access-denied rows surface a null CommandLine
		}
		s, ok := ParseArgs(split(r.CommandLine))
		if !ok {
			continue
		}
		s.PID = r.ProcessId
		out = append(out, s)
	}
	return out, nil
}
