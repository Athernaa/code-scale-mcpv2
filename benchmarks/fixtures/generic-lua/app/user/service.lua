require("app.user.repository")

function validate_user(value)
    return value and value.email ~= nil
end

function persist_user(value)
    if not validate_user(value) then
        return false
    end
    return write(value)
end
