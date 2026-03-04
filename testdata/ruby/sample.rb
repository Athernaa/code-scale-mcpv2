MAX_RETRIES = 3

# Manages user operations.
class UserService
  # Get user by ID.
  def get_user(user_id)
    { id: user_id }
  end

  # Delete a user.
  def delete_user(user_id)
    true
  end
end

# Authenticate a token.
def authenticate(token)
  token.length > 0
end
