BEGIN;
DELETE FROM guacamole_entity WHERE type = 'USER' AND name = 'guacadmin';
INSERT INTO guacamole_entity(name, type) VALUES ('vendor', 'USER'), ('auditor', 'USER');
-- A one-byte value can never match the JDBC SHA-256 password verifier.
INSERT INTO guacamole_user(entity_id, password_hash, password_date)
SELECT entity_id, decode('00', 'hex'), CURRENT_TIMESTAMP
FROM guacamole_entity WHERE type = 'USER' AND name IN ('vendor', 'auditor');
INSERT INTO guacamole_connection(connection_name, protocol) VALUES ('RTAC SSH (brokered)', 'ssh');
INSERT INTO guacamole_connection_parameter(connection_id, parameter_name, parameter_value)
SELECT connection_id, p.k, p.v FROM guacamole_connection CROSS JOIN (VALUES
  ('hostname', :'router'), ('port', '2222'),
  ('username', 'rangerdanger'), ('password', 'rangerdanger'),
  ('recording-path', '/recordings/${HISTORY_UUID}'),
  ('recording-name', 'session'), ('create-recording-path', 'true')
) AS p(k,v) WHERE connection_name = 'RTAC SSH (brokered)';
INSERT INTO guacamole_connection_permission(entity_id, connection_id, permission)
SELECT e.entity_id, c.connection_id, 'READ'
FROM guacamole_entity e CROSS JOIN guacamole_connection c
WHERE e.name = 'vendor' AND e.type = 'USER' AND c.connection_name = 'RTAC SSH (brokered)';
INSERT INTO guacamole_system_permission(entity_id, permission)
SELECT entity_id, 'AUDIT' FROM guacamole_entity WHERE name = 'auditor' AND type = 'USER';
CREATE TABLE sra_seed_status (ready integer NOT NULL CHECK (ready = 1));
INSERT INTO sra_seed_status VALUES (1);
COMMIT;
