package protocol

import (
	"encoding/json"
	"time"
)

const (
	MsgHello            = "hello"
	MsgHelloAck         = "hello_ack"
	MsgTargetHints      = "target_hints"
	MsgObservationBatch = "observation_batch"
	MsgObservationAck   = "observation_ack"
	MsgHeartbeat        = "heartbeat"
	MsgTriggerCollect   = "trigger_collect"
)

// Envelope wraps every WebSocket frame exchanged between collectors and the server.
type Envelope struct {
	ID          string          `json:"id,omitempty"`
	Type        string          `json:"type"`
	Timestamp   time.Time       `json:"timestamp"`
	CollectorID string          `json:"collector_id,omitempty"`
	Payload     json.RawMessage `json:"payload"`
}

func NewEnvelope(msgType, collectorID string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		Type:        msgType,
		Timestamp:   time.Now().UTC(),
		CollectorID: collectorID,
		Payload:     raw,
	}, nil
}

type HelloPayload struct {
	CollectorID string            `json:"collector_id"`
	Name        string            `json:"name"`
	Site        string            `json:"site"`
	Zone        string            `json:"zone,omitempty"`
	Version     string            `json:"version"`
	Hostname    string            `json:"hostname,omitempty"`
	Sources     []string          `json:"sources"`
	Labels      map[string]string `json:"labels,omitempty"`
}

type HelloAckPayload struct {
	Accepted                 bool   `json:"accepted"`
	ServerVersion            string `json:"server_version"`
	HeartbeatIntervalSeconds int    `json:"heartbeat_interval_seconds"`
	Message                  string `json:"message,omitempty"`
}

type TargetHintsPayload struct {
	Revision string       `json:"revision"`
	Hints    []TargetHint `json:"hints"`
}

type TargetHint struct {
	DeviceName        string   `json:"device_name"`
	Site              string   `json:"site"`
	Role              string   `json:"role,omitempty"`
	DeviceType        string   `json:"device_type,omitempty"`
	Tags              []string `json:"tags,omitempty"`
	Collector         string   `json:"collector,omitempty"`
	SNMPIP            string   `json:"snmp_ip,omitempty"`
	SNMPCredential    string   `json:"snmp_credential,omitempty"`
	SNMPInterfaces    bool     `json:"snmp_interfaces,omitempty"`
	SNMPCables        bool     `json:"snmp_cables,omitempty"`
	RedfishIP         string   `json:"redfish_ip,omitempty"`
	RedfishCredential string   `json:"redfish_credential,omitempty"`
	IPMIIP            string   `json:"ipmi_ip,omitempty"`
	IPMICredential    string   `json:"ipmi_credential,omitempty"`
	SSHHost           string   `json:"ssh_host,omitempty"`
	SSHCredential     string   `json:"ssh_credential,omitempty"`
	UniFiIP           string   `json:"unifi_ip,omitempty"`
	UniFiMAC          string   `json:"unifi_mac,omitempty"`
	UniFiSerial       string   `json:"unifi_serial,omitempty"`
	TalosNode         string   `json:"talos_node,omitempty"`
	TalosCluster      string   `json:"talos_cluster,omitempty"`
	OOBIP             string   `json:"oob_ip,omitempty"`
	MACs              []string `json:"macs,omitempty"`
}

type ObservationAckPayload struct {
	BatchID          string `json:"batch_id"`
	SnapshotID       int64  `json:"snapshot_id,omitempty"`
	Changed          bool   `json:"changed"`
	ProcessedDevices int    `json:"processed_devices"`
	ProcessedLinks   int    `json:"processed_links"`
	Error            string `json:"error,omitempty"`
}

type HeartbeatPayload struct {
	CollectorID   string                `json:"collector_id"`
	UptimeSeconds int64                 `json:"uptime_seconds"`
	QueueDepth    int                   `json:"queue_depth"`
	Sources       []SourceHealthPayload `json:"sources,omitempty"`
}

type SourceHealthPayload struct {
	Source        string     `json:"source"`
	Profile       string     `json:"profile"`
	Enabled       bool       `json:"enabled"`
	Status        string     `json:"status"` // ok | error | running | disabled
	LastRunAt     *time.Time `json:"last_run_at,omitempty"`
	LastSuccessAt *time.Time `json:"last_success_at,omitempty"`
	DurationMs    int64      `json:"duration_ms"`
	DeviceCount   int        `json:"device_count"`
	LinkCount     int        `json:"link_count"`
	Error         string     `json:"error,omitempty"`
}

type TriggerCollectPayload struct {
	Sources []string `json:"sources,omitempty"`
}
