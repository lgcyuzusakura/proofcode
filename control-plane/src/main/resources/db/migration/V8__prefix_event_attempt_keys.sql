UPDATE task_events
SET event_key = '1:' || event_key
WHERE event_key IS NOT NULL
  AND event_key NOT LIKE '%:%:%';
