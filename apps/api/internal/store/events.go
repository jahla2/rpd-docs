package store

import (
    "context"
    "encoding/json"
    "time"
)

type JobEvent struct {
    ID int64 `json:"id"`
    EventType string `json:"event_type"`
    Status string `json:"status"`
    Progress int `json:"progress"`
    PageNumber *int `json:"page_number,omitempty"`
    TotalPages *int `json:"total_pages,omitempty"`
    Data json.RawMessage `json:"data,omitempty"`
    CreatedAt time.Time `json:"created_at"`
}

type ProcessingPage struct {
    PageNumber int `json:"page_number"`
    Route string `json:"route"`
    SearchableText bool `json:"searchable_text"`
    Status string `json:"status"`
    ImageArtifact *string `json:"image_artifact,omitempty"`
    TextLayoutArtifact *string `json:"text_layout_artifact,omitempty"`
    OCRArtifact *string `json:"ocr_artifact,omitempty"`
    TableArtifact *string `json:"table_artifact,omitempty"`
    UpdatedAt time.Time `json:"updated_at"`
}

func (s *Store) ListJobEventsAfter(
    ctx context.Context,
    tenantID,jobID string,
    afterID int64,
    limit int,
)([]JobEvent,error){
    if limit<=0 || limit>500 { limit=100 }

    rows,err:=s.pool.Query(ctx,`
      SELECT id,event_type,status,progress,page_number,total_pages,data_json,created_at
      FROM job_events
      WHERE tenant_id=$1 AND job_id=$2 AND id>$3
      ORDER BY id
      LIMIT $4
    `,tenantID,jobID,afterID,limit)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]JobEvent,0)
    for rows.Next(){
        var event JobEvent
        if err:=rows.Scan(
            &event.ID,&event.EventType,&event.Status,&event.Progress,
            &event.PageNumber,&event.TotalPages,&event.Data,&event.CreatedAt,
        ); err!=nil { return nil,err }
        out=append(out,event)
    }
    return out,rows.Err()
}

func (s *Store) ListProcessingPages(ctx context.Context,tenantID,jobID string)([]ProcessingPage,error){
    rows,err:=s.pool.Query(ctx,`
      SELECT page_number,route,searchable_text,status,image_artifact,text_layout_artifact,
             ocr_artifact,table_artifact,updated_at
      FROM processing_pages
      WHERE tenant_id=$1 AND job_id=$2
      ORDER BY page_number
    `,tenantID,jobID)
    if err!=nil { return nil,err }
    defer rows.Close()

    out:=make([]ProcessingPage,0)
    for rows.Next(){
        var page ProcessingPage
        if err:=rows.Scan(
            &page.PageNumber,&page.Route,&page.SearchableText,&page.Status,
            &page.ImageArtifact,&page.TextLayoutArtifact,&page.OCRArtifact,&page.TableArtifact,&page.UpdatedAt,
        ); err!=nil { return nil,err }
        out=append(out,page)
    }
    return out,rows.Err()
}
