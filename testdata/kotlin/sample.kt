// MaxRetries is the retry limit.
const val MAX_RETRIES = 3

// User represents a user.
data class User(val id: Int, val name: String)

// UserService manages user operations.
class UserService {
    // Get user by ID.
    fun getUser(id: Int): User {
        return User(id, "user")
    }

    // Delete a user.
    fun deleteUser(id: Int): Boolean {
        return true
    }
}

// Authenticate a token.
fun authenticate(token: String): Boolean {
    return token.isNotEmpty()
}
