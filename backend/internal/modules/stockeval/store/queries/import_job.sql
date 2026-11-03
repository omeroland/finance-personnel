-- name: InsertImportJob :one
INSERT INTO stockeval.import_job (owner_id, type, status)
VALUES (@owner_id, @type, 'pending')
RETURNING *;

-- name: GetImportJob :one
SELECT * FROM stockeval.import_job WHERE owner_id = @owner_id AND id = @id;

-- name: GetImportJobForUpdate :one
SELECT * FROM stockeval.import_job WHERE owner_id = @owner_id AND id = @id FOR UPDATE;

-- name: ListImportJobs :many
SELECT * FROM stockeval.import_job WHERE owner_id = @owner_id ORDER BY created_at DESC, id LIMIT @max_rows;

-- name: UpdateImportJob :one
UPDATE stockeval.import_job
SET status = @status, report = @report, finished_at = @finished_at
WHERE owner_id = @owner_id AND id = @id
RETURNING *;
