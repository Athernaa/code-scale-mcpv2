local service = require("app.user.service")

function save_user(value)
    return persist_user(value)
end

function health_check()
    return "ok"
end
