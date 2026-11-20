package publisher

import (
	"bytes"
	"compress/gzip"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/honeycombio/honeyaws/state"
	"github.com/honeycombio/honeytail/event"
)

const accessLogLine = `h2 2026-11-19T17:05:15.520000Z app/example/abc123 203.0.113.1:36280 10.0.0.1:8080 0.001 0.003 0.001 200 200 47 258 "GET https://example.com/path HTTP/2.0" "A user agent" TLS_AES_128_GCM_SHA256 TLSv1.3 groupARN "Root=1-abcdef12-1234567890abcdef12345678" "example.com" "certARN" 0 2026-11-19T17:05:15.515000Z "forward" "-" "-" "10.0.0.1:8080" "200" "-" "-"`

const connectionLogLine = `2026-11-19T17:05:15.514108Z 203.0.113.1 36280 443 TLSv1.3 TLS_AES_128_GCM_SHA256 0.004 "CN=example.com, O=Example Company" NotBefore=2026-01-01T00:00:00Z;NotAfter=2027-01-01T00:00:00Z 000123 Success`

const connectionObject = "logs/AWSLogs/12345/elasticloadbalancing/us-east-1/2026/11/19/conn_log_12345_elasticloadbalancing_us-east-1_app.example.abc123_20261119T1710Z_10.0.0.1_random.log.gz"

func albTestObject(t *testing.T, object, contents string, truncate bool) state.DownloadedObject {
	t.Helper()
	var compressed bytes.Buffer
	zipper := gzip.NewWriter(&compressed)
	if _, err := zipper.Write([]byte(contents + "\n")); err != nil {
		t.Fatal(err)
	}
	if err := zipper.Close(); err != nil {
		t.Fatal(err)
	}
	data := compressed.Bytes()
	if truncate {
		data = data[:len(data)-8]
	}
	filename := filepath.Join(t.TempDir(), "log.gz")
	if err := os.WriteFile(filename, data, 0600); err != nil {
		t.Fatal(err)
	}
	return state.DownloadedObject{Filename: filename, Object: object}
}

func receiveALBEvent(t *testing.T, out <-chan event.Event) event.Event {
	t.Helper()
	select {
	case ev := <-out:
		return ev
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for ALB event")
		return event.Event{}
	}
}

func parseAccessTestEvent(t *testing.T, line string) event.Event {
	t.Helper()
	out := make(chan event.Event, 1)
	if err := (&ALBEventParser{}).ParseEvents(albTestObject(t, "access.log.gz", line, false), out); err != nil {
		t.Fatal(err)
	}
	return receiveALBEvent(t, out)
}

func TestALBAccessConnectionID(t *testing.T) {
	for _, tc := range []struct{ name, suffix, id string }{
		{"legacy", "", ""},
		{"missing", " -", ""},
		{"present", " TID_connection", "TID_connection"},
		{"opaque numeric", " 000123", "000123"},
		{"future fields", ` TID_connection "new host" "new URI" - "ignored"`, "TID_connection"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev := parseAccessTestEvent(t, accessLogLine+tc.suffix)
			if got, _ := ev.Data["conn_trace_id"].(string); got != tc.id {
				t.Fatalf("connection ID = %#v; want %q", ev.Data["conn_trace_id"], tc.id)
			}
			if tc.id == "" {
				if _, exists := ev.Data["conn_trace_id"]; exists {
					t.Fatal("missing ID should be absent")
				}
			}

			if ev.Data["log_type"] != "access" || ev.Data["user_agent"] != "A user agent" {
				t.Fatalf("unexpected access fields: %#v", ev.Data)
			}
		})
	}
}

func TestALBConnectionFormats(t *testing.T) {
	for _, tc := range []struct {
		name, suffix string
		extra        map[string]interface{}
	}{
		{"legacy", "", nil},
		{"connection ID", " TID_connection", map[string]interface{}{"conn_trace_id": "TID_connection"}},
		{"key exchange", " TID_connection X25519", map[string]interface{}{"conn_trace_id": "TID_connection", "tls_keyexchange": "X25519"}},
		{"current", " TID_connection X25519 app/from-record/def456 10.0.0.2", map[string]interface{}{"conn_trace_id": "TID_connection", "tls_keyexchange": "X25519", "elb": "app/from-record/def456", "ip_address": "10.0.0.2"}},
		{"future fields", " TID_connection X25519 app/example/abc123 10.0.0.1 ignored", map[string]interface{}{"conn_trace_id": "TID_connection", "tls_keyexchange": "X25519", "ip_address": "10.0.0.1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ev, err := parseALBConnectionLine(connectionLogLine+tc.suffix, connectionObject)
			if err != nil {
				t.Fatal(err)
			}
			want := map[string]interface{}{
				"log_type": "connection", "elb": "app/example/abc123", "client_ip": "203.0.113.1",
				"client_port": int64(36280), "listener_port": int64(443), "tls_protocol": "TLSv1.3",
				"tls_cipher": "TLS_AES_128_GCM_SHA256", "tls_handshake_latency": 0.004,
				"leaf_client_cert_subject":       "CN=example.com, O=Example Company",
				"leaf_client_cert_validity":      "NotBefore=2026-01-01T00:00:00Z;NotAfter=2027-01-01T00:00:00Z",
				"leaf_client_cert_serial_number": "000123", "tls_verify_status": "Success",
			}
			for key, value := range tc.extra {
				want[key] = value
			}
			if !reflect.DeepEqual(ev.Data, want) {
				t.Fatalf("data = %#v; want %#v", ev.Data, want)
			}
			if ev.Timestamp.Format(time.RFC3339Nano) != "2026-11-19T17:05:15.514108Z" {
				t.Fatal("wrong timestamp")
			}
		})
	}
}

func TestALBTraceIDs(t *testing.T) {
	ev := parseAccessTestEvent(t, accessLogLine+" TID_connection")
	addTraceData(&ev, false)
	if ev.Data["trace.trace_id"] != "1-abcdef12-1234567890abcdef12345678" || ev.Data["conn_trace_id"] != "TID_connection" {
		t.Fatalf("request and connection IDs were not preserved: %#v", ev.Data)
	}
	connection, err := parseALBConnectionLine(connectionLogLine+" TID_connection", connectionObject)
	if err != nil {
		t.Fatal(err)
	}
	addTraceData(&connection, false)
	if _, exists := connection.Data["trace.trace_id"]; exists {
		t.Fatal("connection should not create an application trace")
	}
}

func TestALBConnectionEscapedSubject(t *testing.T) {
	line := strings.Replace(connectionLogLine, `"CN=example.com, O=Example Company"`, `"CN=\"Example\", O=Example Company"`, 1)
	ev, err := parseALBConnectionLine(line, connectionObject)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data["leaf_client_cert_subject"] != `CN=\"Example\", O=Example Company` {
		t.Fatalf("subject = %#v", ev.Data["leaf_client_cert_subject"])
	}
}

func TestALBAccessConnectionIDsStayWithRequests(t *testing.T) {
	var lines []string
	for i := range 16 {
		line := strings.Replace(accessLogLine, "/path", fmt.Sprintf("/request/%d", i), 1)
		lines = append(lines, fmt.Sprintf("%s TID_%d", line, i))
	}
	out := make(chan event.Event, len(lines))
	if err := (&ALBEventParser{}).ParseEvents(albTestObject(t, "access.log.gz", strings.Join(lines, "\n"), false), out); err != nil {
		t.Fatal(err)
	}
	seen := make(map[string]bool)
	for range lines {
		ev := receiveALBEvent(t, out)
		id := ev.Data["conn_trace_id"].(string)
		request := fmt.Sprintf("GET https://example.com/request/%s HTTP/2.0", strings.TrimPrefix(id, "TID_"))
		if ev.Data["request"] != request || seen[id] {
			t.Fatalf("connection ID attached to wrong request: %#v", ev.Data)
		}
		seen[id] = true
	}
}

func TestALBAccessTimestampFallback(t *testing.T) {
	before := time.Now()
	line := strings.Replace(accessLogLine, "2026-11-19T17:05:15.515000Z", "bad-time", 1)
	ev := parseAccessTestEvent(t, line+" TID_connection")
	if ev.Timestamp.Before(before) || ev.Timestamp.After(time.Now()) {
		t.Fatalf("nginx timestamp fallback changed: %v", ev.Timestamp)
	}
}

func TestALBConnectionFailureAndMissingFields(t *testing.T) {
	ev, err := parseALBConnectionLine(`2026-11-19T17:05:15Z 2001:db8::1 36280 443 TLSv1.3 TLS_AES_128_GCM_SHA256 - "-" - - Failed:ClientCertUntrusted TID_failed -`, connectionObject)
	if err != nil {
		t.Fatal(err)
	}
	if ev.Data["tls_verify_status"] != "Failed:ClientCertUntrusted" || ev.Data["conn_trace_id"] != "TID_failed" || ev.Data["client_ip"] != "2001:db8::1" {
		t.Fatalf("unexpected failure fields: %#v", ev.Data)
	}
	for _, field := range []string{"tls_handshake_latency", "leaf_client_cert_subject", "leaf_client_cert_validity", "leaf_client_cert_serial_number", "tls_keyexchange"} {
		if _, exists := ev.Data[field]; exists {
			t.Errorf("missing %s should be absent", field)
		}
	}
}

func TestALBMalformedConnectionLines(t *testing.T) {
	for _, line := range []string{"", "too few fields", strings.Replace(connectionLogLine, "36280", "bad-port", 1), strings.Replace(connectionLogLine, "0.004", "NaN", 1), strings.Replace(connectionLogLine, "0.004", "-1", 1)} {
		if _, err := parseALBConnectionLine(line, connectionObject); err == nil {
			t.Errorf("accepted malformed connection log %q", line)
		}
	}
}

func TestALBParseObjects(t *testing.T) {
	access := accessLogLine + " TID_shared"
	for _, tc := range []struct {
		name, object, contents string
		want                   int
		wantErr                bool
		truncate               bool
	}{
		{"access", "access.log.gz", access + "\n" + strings.Replace(access, "/path", "/other-path", 1), 2, false, false},
		{"connection", connectionObject, connectionLogLine + " TID_shared", 1, false, false},
		{"empty", connectionObject, "# header\n\n", 0, false, false},
		{"malformed between valid records", "access.log.gz", access + "\ninvalid log line\n" + access, 2, false, false},
		{"truncated gzip", connectionObject, connectionLogLine + " TID_shared", 1, true, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := make(chan event.Event, 2)
			object := albTestObject(t, tc.object, tc.contents, tc.truncate)
			err := (&ALBEventParser{}).ParseEvents(object, out)
			if (err != nil) != tc.wantErr {
				t.Fatalf("error = %v; want error = %v", err, tc.wantErr)
			}
			kind := "access"
			if tc.object == connectionObject {
				kind = "connection"
			}
			for range tc.want {
				ev := receiveALBEvent(t, out)
				if ev.Data["log_type"] != kind || ev.Data["conn_trace_id"] != "TID_shared" {
					t.Fatalf("unexpected event: %#v", ev)
				}
			}
		})
	}
}
