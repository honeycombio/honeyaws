package publisher

import (
	"fmt"
	"math"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/honeycombio/honeytail/event"
)

var albConnectionFields = []string{
	"timestamp", "client_ip", "client_port", "listener_port", "tls_protocol", "tls_cipher",
	"tls_handshake_latency", "leaf_client_cert_subject", "leaf_client_cert_validity",
	"leaf_client_cert_serial_number", "tls_verify_status", "conn_trace_id", "tls_keyexchange",
	"elb", "ip_address",
}

// ALB adds fields at the end of each format. Read only the fields we understand,
// accepting older records that predate conn_trace_id and later connection fields.
func splitALBFields(line string, limit int) ([]string, error) {
	fields := make([]string, 0, limit)
	for i := 0; i < len(line) && len(fields) < limit; {
		if line[i] == ' ' || line[i] == '\t' {
			i++
			continue
		}
		start := i
		if line[i] == '"' {
			i++
			start = i
			for i < len(line) && line[i] != '"' {
				if line[i] == '\\' {
					i++
				}
				i++
			}
			if i >= len(line) {
				return nil, fmt.Errorf("unterminated quoted ALB field")
			}
			fields = append(fields, line[start:i])
			i++
			if i < len(line) && line[i] != ' ' && line[i] != '\t' {
				return nil, fmt.Errorf("missing separator after quoted ALB field")
			}
		} else {
			for i < len(line) && line[i] != ' ' && line[i] != '\t' {
				i++
			}
			fields = append(fields, line[start:i])
		}
	}
	return fields, nil
}

func accessLogWithConnectionID(line string) string {
	fields, err := splitALBFields(line, 30)
	if err == nil && len(fields) == 30 && fields[29] != "-" && fields[29] != "" {
		return "conn_trace_id=" + fields[29] + " " + line
	}
	return line
}

func parseALBConnectionLine(line, objectKey string) (event.Event, error) {
	fields, err := splitALBFields(line, len(albConnectionFields))
	if err != nil {
		return event.Event{}, err
	}
	if len(fields) < 11 {
		return event.Event{}, fmt.Errorf("connection log has %d fields; need at least 11", len(fields))
	}
	timestamp, err := time.Parse(time.RFC3339Nano, fields[0])
	if err != nil {
		return event.Event{}, fmt.Errorf("invalid connection timestamp: %w", err)
	}
	data := map[string]interface{}{"log_type": "connection"}
	for i, value := range fields {
		name := albConnectionFields[i]
		if value == "-" || name == "timestamp" {
			continue
		}
		switch name {
		case "client_port", "listener_port":
			number, err := strconv.ParseInt(value, 10, 64)
			if err != nil || number < 0 || number > 65535 {
				return event.Event{}, fmt.Errorf("invalid connection %s", name)
			}
			data[name] = number
		case "tls_handshake_latency":
			number, err := strconv.ParseFloat(value, 64)
			if err != nil || number < 0 || math.IsNaN(number) || math.IsInf(number, 0) {
				return event.Event{}, fmt.Errorf("invalid connection %s", name)
			}
			data[name] = number
		default:
			data[name] = value
		}
	}
	// Older connection records omit elb. Their S3 filename includes the same
	// resource ID with dots replacing slashes (app.name.id).
	if _, ok := data["elb"]; !ok {
		parts := strings.Split(path.Base(objectKey), "_")
		if len(parts) > 5 && strings.HasPrefix(parts[5], "app.") {
			data["elb"] = strings.ReplaceAll(parts[5], ".", "/")
		}
	}
	return event.Event{Timestamp: timestamp, Data: data}, nil
}
