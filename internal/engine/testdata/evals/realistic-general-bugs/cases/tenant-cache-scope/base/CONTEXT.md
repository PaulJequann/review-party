# Domain context

`TenantID` is the global account boundary. `UserID` values come from each
tenant's identity provider and are unique only within that tenant. Every cache
and database lookup for user data must preserve both identifiers.
