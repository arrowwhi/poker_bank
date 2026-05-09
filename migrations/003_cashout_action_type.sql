-- +goose Up

ALTER TABLE pending_actions DROP CONSTRAINT pending_actions_action_type_check;
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_action_type_check
    CHECK (action_type IN ('JOIN', 'REBUY', 'CASHOUT'));

-- +goose Down

ALTER TABLE pending_actions DROP CONSTRAINT pending_actions_action_type_check;
ALTER TABLE pending_actions ADD CONSTRAINT pending_actions_action_type_check
    CHECK (action_type IN ('JOIN', 'REBUY'));
