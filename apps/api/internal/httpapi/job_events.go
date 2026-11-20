package httpapi

import (
    "encoding/json"
    "fmt"
    "net/http"
    "strconv"
    "strings"
    "time"
)

func (s *Server) listJobPages(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    if _,err:=s.store.GetJob(r.Context(),tenantID,r.PathValue("id")); err!=nil {
        if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","job not found") } else {
            writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read job")
        }
        return
    }

    pages,err:=s.store.ListProcessingPages(r.Context(),tenantID,r.PathValue("id"))
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to list processing pages"); return }
    writeJSON(w,http.StatusOK,map[string]any{"items":pages})
}

func (s *Server) streamJobEvents(w http.ResponseWriter,r *http.Request){
    tenantID:=tenantFrom(r,s.defaultTenantID)
    jobID:=r.PathValue("id")

    job,err:=s.store.GetJob(r.Context(),tenantID,jobID)
    if storeNotFound(err) { writeError(w,http.StatusNotFound,"NOT_FOUND","job not found"); return }
    if err!=nil { writeError(w,http.StatusInternalServerError,"DATABASE_FAILED","failed to read job"); return }

    flusher,ok:=w.(http.Flusher)
    if !ok {
        writeError(w,http.StatusInternalServerError,"SSE_UNSUPPORTED","streaming is not supported by the HTTP writer"); return
    }

    afterID:=int64(0)
    if value:=strings.TrimSpace(r.Header.Get("Last-Event-ID")); value!="" {
        if parsed,parseErr:=strconv.ParseInt(value,10,64); parseErr==nil && parsed>=0 { afterID=parsed }
    }
    if value:=strings.TrimSpace(r.URL.Query().Get("after")); value!="" {
        if parsed,parseErr:=strconv.ParseInt(value,10,64); parseErr==nil && parsed>=0 && parsed>afterID { afterID=parsed }
    }

    w.Header().Set("Content-Type","text/event-stream")
    w.Header().Set("Cache-Control","no-cache, no-transform")
    w.Header().Set("Connection","keep-alive")
    w.Header().Set("X-Accel-Buffering","no")
    w.WriteHeader(http.StatusOK)
    fmt.Fprintf(w,": connected job=%s\n\n",jobID)
    flusher.Flush()

    poll:=time.NewTicker(500*time.Millisecond)
    heartbeat:=time.NewTicker(15*time.Second)
    defer poll.Stop()
    defer heartbeat.Stop()

    terminalSeen:=isTerminalJobStatus(job.Status)

    sendPending:=func() (int,error) {
        events,eventErr:=s.store.ListJobEventsAfter(r.Context(),tenantID,jobID,afterID,200)
        if eventErr!=nil { return 0,eventErr }
        for _,event:=range events {
            payload,_:=json.Marshal(event)
            if _,writeErr:=fmt.Fprintf(w,"id: %d\nevent: %s\ndata: %s\n\n",event.ID,event.EventType,payload); writeErr!=nil {
                return 0,writeErr
            }
            afterID=event.ID
            if isTerminalJobStatus(event.Status) { terminalSeen=true }
        }
        if len(events)>0 { flusher.Flush() }
        return len(events),nil
    }

    if _,err:=sendPending(); err!=nil { return }

    for {
        select {
        case <-r.Context().Done():
            return
        case <-heartbeat.C:
            if _,err:=fmt.Fprint(w,": heartbeat\n\n"); err!=nil { return }
            flusher.Flush()
        case <-poll.C:
            count,err:=sendPending()
            if err!=nil { return }
            if count==0 {
                current,currentErr:=s.store.GetJob(r.Context(),tenantID,jobID)
                if currentErr==nil && isTerminalJobStatus(current.Status) {
                    terminalSeen=true
                }
                if terminalSeen {
                    fmt.Fprint(w,": stream-complete\n\n")
                    flusher.Flush()
                    return
                }
            }
        }
    }
}

func isTerminalJobStatus(status string) bool {
    switch status {
    case "READY_FOR_RECONCILIATION","REVIEW_REQUIRED","COMPLETED","FAILED","CANCELLED":
        return true
    default:
        return false
    }
}
