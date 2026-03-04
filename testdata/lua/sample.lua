-- Get user by ID.
function get_user(id)
    return { id = id }
end

-- Authenticate a token.
local function authenticate(token)
    return #token > 0
end
