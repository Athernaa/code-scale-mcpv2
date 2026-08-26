exports('GetPlayer', function(source)
    return { source = source }
end)

exports('GetCoreObject', function()
    return { Functions = { GetPlayer = exports } }
end)

exports('AddMoney', function(source, amount)
    return source, amount
end)

RegisterNetEvent('core:playerLoaded')
AddEventHandler('core:playerLoaded', function()
    return true
end)
