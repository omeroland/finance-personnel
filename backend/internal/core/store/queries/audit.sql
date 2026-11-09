-- name: InsertAudit :exec
INSERT INTO core.audit_log (owner_id, actor, action, entity, before, after)
VALUES (@owner_id, @actor, @action, @entity, @before, @after);
