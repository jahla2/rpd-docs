package auth

import (
    "sync"
    "time"
)

type rateBucket struct {
    tokens float64
    last time.Time
    lastSeen time.Time
}

type RateLimiter struct {
    mu sync.Mutex
    rate float64
    burst float64
    buckets map[string]*rateBucket
}

func NewRateLimiter(rate float64,burst int)*RateLimiter{
    if rate<=0 { rate=20 }
    if burst<=0 { burst=40 }
    return &RateLimiter{rate:rate,burst:float64(burst),buckets:map[string]*rateBucket{}}
}

func (l *RateLimiter) Allow(key string) bool {
    now:=time.Now()
    l.mu.Lock()
    defer l.mu.Unlock()

    bucket,ok:=l.buckets[key]
    if !ok {
        bucket=&rateBucket{tokens:l.burst,last:now,lastSeen:now}
        l.buckets[key]=bucket
    }

    elapsed:=now.Sub(bucket.last).Seconds()
    bucket.tokens+=elapsed*l.rate
    if bucket.tokens>l.burst { bucket.tokens=l.burst }
    bucket.last=now
    bucket.lastSeen=now

    if len(l.buckets)>10000 {
        cutoff:=now.Add(-15*time.Minute)
        for id,candidate:=range l.buckets {
            if candidate.lastSeen.Before(cutoff) { delete(l.buckets,id) }
        }
    }

    if bucket.tokens<1 { return false }
    bucket.tokens--
    return true
}
