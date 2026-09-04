-- Let volunteer coordinators opt an event into immediate self-scheduling.
-- The default preserves the existing review-then-promote workflow.
ALTER TABLE conferences
ADD COLUMN volunteer_self_schedule boolean NOT NULL DEFAULT false;
