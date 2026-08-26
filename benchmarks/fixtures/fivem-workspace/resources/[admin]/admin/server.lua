function auditPlayer(source)
    return exports.missing_identity:Get(source)
end

RegisterCommand('admin:inspect', function(source)
    return auditPlayer(source)
end)
