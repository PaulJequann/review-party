# Ledger

`Repository.Transfer` is the sole owner and caller of the unexported transfer
statement. A transfer must debit and credit atomically.
