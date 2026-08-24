RegisterNetEvent('avenlo:createCharacter')
AddEventHandler('avenlo:createCharacter', function(data)
    TriggerClientEvent('avenlo:characterCreated', source, data)
end)

RegisterNetEvent('avenlo:unrelated')
AddEventHandler('avenlo:unrelated', function()
end)

lib.callback.register('avenlo:getCharacter', function(source)
    return exports['qbx_core']:GetPlayer(source)
end)

RegisterCommand('revive', function(source, args)
end, false)

exports('getCharacter', function(source)
    return source
end)

exports('getCharacterById', getCharacter)
