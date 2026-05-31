-- sqlc 查询：device-svc 落库 SQL（与 internal/store/store.go 中 const 一致）。

-- name: GetDevice :one
SELECT id, COALESCE(room_id, ''), COALESCE(owner_id, ''), device_type, hardware_model,
       COALESCE(firmware_version, ''), COALESCE(firmware_target, ''), COALESCE(certificate_id, ''),
       online_status, last_seen_at, remaining_food_grams, health_status,
       COALESCE(mqtt_username, ''), COALESCE(mqtt_credential_hash, ''), mqtt_credential_expires_at,
       COALESCE(capability, '{}'::jsonb), region_id, created_at, updated_at
FROM devices WHERE id = $1;

-- name: CreateDevice :exec
INSERT INTO devices
(id, room_id, owner_id, device_type, hardware_model, firmware_version, firmware_target, certificate_id,
 online_status, last_seen_at, remaining_food_grams, health_status,
 mqtt_username, mqtt_credential_hash, mqtt_credential_expires_at,
 capability, region_id, created_at, updated_at)
VALUES
($1, NULLIF($2,''), NULLIF($3,''), $4, $5, NULLIF($6,''), NULLIF($7,''), NULLIF($8,''),
 $9, $10, $11, $12,
 NULLIF($13,''), NULLIF($14,''), $15,
 $16::jsonb, $17, $18, $18)
ON CONFLICT (id) DO NOTHING;

-- name: MarkOnline :exec
UPDATE devices SET
  online_status = 'online',
  last_seen_at = $2,
  remaining_food_grams = CASE WHEN $3 > 0 THEN $3 ELSE remaining_food_grams END,
  updated_at = $2
WHERE id = $1;

-- name: BindRoom :exec
UPDATE devices SET room_id = $2, updated_at = NOW() WHERE id = $1;

-- name: UnbindRoom :exec
UPDATE devices SET room_id = NULL, updated_at = NOW() WHERE id = $1;

-- name: SetMqttCredential :exec
UPDATE devices SET
  mqtt_username = $2,
  mqtt_credential_hash = $3,
  mqtt_credential_expires_at = $4,
  updated_at = NOW()
WHERE id = $1;

-- name: SetFirmwareTarget :exec
UPDATE devices SET firmware_target = NULLIF($2,''), updated_at = NOW() WHERE id = $1;

-- name: SetDeviceStatus :exec
UPDATE devices SET online_status = $2, updated_at = NOW() WHERE id = $1;
