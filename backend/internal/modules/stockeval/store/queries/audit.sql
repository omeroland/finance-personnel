-- name: InsertAudit :exec
INSERT INTO core.audit_log (owner_id, actor, action, entity, before, after)
VALUES (@owner_id, @actor, @action, @entity, @before, @after);

-- name: CountAudit :one
SELECT count(*) FROM core.audit_log WHERE owner_id = @owner_id;
