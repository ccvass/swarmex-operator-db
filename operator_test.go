package operatordb

import "testing"

func TestParseDBConfig(t *testing.T) {
	tests := []struct {
		name   string
		labels map[string]string
		want   DBConfig
	}{
		{"defaults", map[string]string{}, DBConfig{Engine: "postgres", Port: 5432, MinReady: 1}},
		{"mysql", map[string]string{labelEngine: "mysql"}, DBConfig{Engine: "mysql", Port: 3306, MinReady: 1}},
		{"custom port", map[string]string{labelEngine: "postgres", labelPort: "5433", labelMinReady: "2"}, DBConfig{Engine: "postgres", Port: 5433, MinReady: 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseDBConfig(tt.labels)
			if got != tt.want {
				t.Errorf("got %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestTcpCheck(t *testing.T) {
	// Should fail on unreachable host
	if tcpCheck("192.0.2.1", 9999) {
		t.Error("expected false for unreachable host")
	}
}
