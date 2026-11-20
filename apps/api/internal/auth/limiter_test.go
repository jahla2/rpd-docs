package auth

import "testing"

func TestRateLimiterBurst(t *testing.T){
    limiter:=NewRateLimiter(0.0001,2)
    if !limiter.Allow("key") || !limiter.Allow("key") { t.Fatal("initial burst should pass") }
    if limiter.Allow("key") { t.Fatal("third immediate request should be limited") }
}
