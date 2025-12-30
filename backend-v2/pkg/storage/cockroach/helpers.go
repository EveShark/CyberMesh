package cockroach

// isPrimaryKeyViolation checks if error is a primary key or unique constraint violation
func isPrimaryKeyViolation(err error) bool {
	return isUniqueViolation(err, "")
}
