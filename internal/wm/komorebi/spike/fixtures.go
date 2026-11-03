//go:build windows

package main

import (
	"encoding/json/v2"
	"fmt"
	"path/filepath"
)

func scrubJSON(source []byte) ([]byte, error) {
	var value any
	if err := json.Unmarshal(source, &value); err != nil {
		return nil, fmt.Errorf("decoding fixture for scrubbing: %w", err)
	}
	windowNumber, handleNumber, deviceNumber := 0, 0, 0
	devices := make(map[string]string)
	var scrub func(any, string)
	scrub = func(value any, parentKey string) {
		switch item := value.(type) {
		case map[string]any:
			for key, child := range item {
				switch key {
				case "title":
					windowNumber++
					item[key] = fmt.Sprintf("window-%d", windowNumber)
				case "hwnd":
					handleNumber++
					item[key] = handleNumber
				case "exe":
					if executable, ok := child.(string); ok {
						item[key] = filepath.Base(executable)
					}
				case "device", "device_id", "deviceId", "serial_number_id", "serial":
					if identifier, ok := child.(string); ok && identifier != "" {
						redacted, exists := devices[identifier]
						if !exists {
							deviceNumber++
							redacted = fmt.Sprintf("device-%d", deviceNumber)
							devices[identifier] = redacted
						}
						item[key] = redacted
					}
				case "path", "wallpaper", "configuration_dir", "data_dir":
					if _, ok := child.(string); ok {
						item[key] = "<path>"
					}
				default:
					scrub(child, key)
				}
			}
		case []any:
			for _, child := range item {
				scrub(child, parentKey)
			}
		}
	}
	scrub(value, "")
	result, err := json.Marshal(value)
	if err != nil {
		return nil, fmt.Errorf("encoding scrubbed fixture: %w", err)
	}
	return result, nil
}
