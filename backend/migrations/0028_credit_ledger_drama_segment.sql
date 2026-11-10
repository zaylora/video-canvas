ALTER TABLE credit_ledger ADD COLUMN drama_segment_id bigint;
CREATE INDEX idx_ledger_drama_segment ON credit_ledger (drama_segment_id) WHERE drama_segment_id IS NOT NULL;
