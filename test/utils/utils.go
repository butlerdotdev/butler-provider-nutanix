/*
Copyright 2026 The Butler Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

// Package utils contains test utilities.
package utils

import (
	"os"
	"os/exec"
)

// Run executes a command and returns its output.
func Run(cmd *exec.Cmd) (string, error) {
	output, err := cmd.CombinedOutput()
	return string(output), err
}

// GetEnvOrDefault returns the value of an environment variable or a default value.
func GetEnvOrDefault(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}
