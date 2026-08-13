BEGIN;
UPDATE accounts SET balance = balance - :amount WHERE id = :sender;
UPDATE accounts SET balance = balance + :amount WHERE id = :recipient;
COMMIT;
