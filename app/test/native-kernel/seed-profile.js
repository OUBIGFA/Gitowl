// This entry runs only in the dedicated kernel integration build. Seed an
// isolated Chromium profile without contacting telemetry or inheriting accounts.
localStorage.setItem('stats-opt-out', '1')
localStorage.setItem('has-sent-stats-opt-in-ping', '1')
localStorage.setItem('has-shown-welcome-flow', '1')
