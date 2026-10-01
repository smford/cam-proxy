package main

import "testing"

func TestVersionVariables(t *testing.T) {
	if version == "" {
		t.Errorf("expected version to have default value, got empty")
	}
	if commit == "" {
		t.Errorf("expected commit to have default value, got empty")
	}
	if date == "" {
		t.Errorf("expected date to have default value, got empty")
	}
}
