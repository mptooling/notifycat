// Package domain holds the validation domain's contracts: the ports that
// infrastructure adapters satisfy, the DTOs and enums that describe a
// validation run, and the constants that define what "valid" means. It is
// pure — it imports only the standard library, the shared kernel, and the
// routing domain (whose Entry and RepoMapping value objects it references).
package domain
