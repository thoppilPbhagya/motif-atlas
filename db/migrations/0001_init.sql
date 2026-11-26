-- 0001_init: turn on pgvector so Postgres can store and search embeddings.
CREATE EXTENSION IF NOT EXISTS vector;