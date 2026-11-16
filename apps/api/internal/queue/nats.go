package queue

import (
    "encoding/json"
    "fmt"

    "github.com/nats-io/nats.go"
)

const StreamName = "RPD_DOCUMENTS"

var streamSubjects=[]string{"document.*","reconciliation.*"}

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

type ReconciliationJobMessage struct {
    SchemaVersion string `json:"schema_version"`
    TenantID string `json:"tenant_id"`
    RunID string `json:"run_id"`
}

func NewPublisher(url string) (*Publisher, error) {
    nc, err := nats.Connect(url, nats.Name("rpd-api"))
    if err != nil { return nil, err }
    js, err := nc.JetStream()
    if err != nil { nc.Close(); return nil, err }
    if err:=ensureStream(js); err!=nil {
        nc.Close()
        return nil,err
    }
    return &Publisher{conn:nc, js:js}, nil
}

func ensureStream(js nats.JetStreamContext) error {
    info,err:=js.StreamInfo(StreamName)
    if err!=nil {
        if _,addErr:=js.AddStream(&nats.StreamConfig{
            Name:StreamName,Subjects:streamSubjects,Storage:nats.FileStorage,
        }); addErr!=nil {
            return fmt.Errorf("ensure stream: %w",addErr)
        }
        return nil
    }

    existing:=map[string]bool{}
    for _,subject:=range info.Config.Subjects { existing[subject]=true }
    changed:=false
    subjects:=append([]string(nil),info.Config.Subjects...)
    for _,subject:=range streamSubjects {
        if !existing[subject] {
            subjects=append(subjects,subject)
            changed=true
        }
    }
    if changed {
        config:=info.Config
        config.Subjects=subjects
        if _,updateErr:=js.UpdateStream(&config); updateErr!=nil {
            return fmt.Errorf("update stream subjects: %w",updateErr)
        }
    }
    return nil
}

func (p *Publisher) Publish(subject string, msg JobMessage) error {
    payload,err:=json.Marshal(msg)
    if err!=nil { return err }
    dedupeKey:=msg.JobID+":"+msg.RunID+":"+msg.Step
    _,err=p.js.Publish(subject,payload,nats.MsgId(dedupeKey))
    return err
}

func (p *Publisher) PublishReconciliation(msg ReconciliationJobMessage) error {
    payload,err:=json.Marshal(msg)
    if err!=nil { return err }
    dedupeKey:="reconciliation:"+msg.RunID
    _,err=p.js.Publish("reconciliation.run",payload,nats.MsgId(dedupeKey))
    return err
}

func (p *Publisher) PublishRaw(subject string,payload []byte,dedupeKey string) error {
    if dedupeKey=="" {
        _,err:=p.js.Publish(subject,payload)
        return err
    }
    _,err:=p.js.Publish(subject,payload,nats.MsgId(dedupeKey))
    return err
}

func (p *Publisher) PublishJSON(subject string,msg any) error {
    payload,err:=json.Marshal(msg)
    if err!=nil { return err }
    _,err=p.js.Publish(subject,payload)
    return err
}

func (p *Publisher) Ready() bool { return p != nil && p.conn != nil && p.conn.IsConnected() }
func (p *Publisher) Close() { if p != nil && p.conn != nil { _ = p.conn.Drain(); p.conn.Close() } }
