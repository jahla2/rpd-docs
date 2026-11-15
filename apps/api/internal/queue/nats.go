package queue

import (
    "encoding/json"
    "fmt"
    "github.com/nats-io/nats.go"
)

const StreamName = "RPD_DOCUMENTS"

type Publisher struct { conn *nats.Conn; js nats.JetStreamContext }
type JobMessage struct {
    SchemaVersion string `json:"schema_version"`
    JobID string `json:"job_id"`
    TenantID string `json:"tenant_id"`
    DocumentID string `json:"document_id"`
    RunID string `json:"run_id"`
    Step string `json:"step"`
    InputArtifact string `json:"input_artifact"`
    Options map[string]any `json:"options,omitempty"`
}

func NewPublisher(url string) (*Publisher, error) {
    nc, err := nats.Connect(url, nats.Name("rpd-api"))
    if err != nil { return nil, err }
    js, err := nc.JetStream()
    if err != nil { nc.Close(); return nil, err }
    if _, err := js.StreamInfo(StreamName); err != nil {
        if _, addErr := js.AddStream(&nats.StreamConfig{
            Name: StreamName, Subjects: []string{"document.*"}, Storage: nats.FileStorage,
        }); addErr != nil {
            nc.Close()
            return nil, fmt.Errorf("ensure stream: %w", addErr)
        }
    }
    return &Publisher{conn:nc, js:js}, nil
}

func (p *Publisher) Publish(subject string, msg JobMessage) error {
    payload, err := json.Marshal(msg); if err != nil { return err }
    _, err = p.js.Publish(subject, payload); return err
}
func (p *Publisher) Ready() bool { return p != nil && p.conn != nil && p.conn.IsConnected() }
func (p *Publisher) Close() { if p != nil && p.conn != nil { _ = p.conn.Drain(); p.conn.Close() } }
