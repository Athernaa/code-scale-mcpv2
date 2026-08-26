local service = require("app.user.service")

function save_user(value)
    return service.persist_user(value)
end

function health_check()
    return "ok"
end
