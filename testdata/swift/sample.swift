// MaxRetries is the retry limit.
let MAX_RETRIES = 3

// User represents a user.
struct User {
    let id: Int
    let name: String
}

// UserService manages user operations.
class UserService {
    // Get user by ID.
    func getUser(id: Int) -> User {
        return User(id: id, name: "user")
    }

    // Delete a user.
    func deleteUser(id: Int) -> Bool {
        return true
    }
}

// Authenticate a token.
func authenticate(token: String) -> Bool {
    return !token.isEmpty
}
