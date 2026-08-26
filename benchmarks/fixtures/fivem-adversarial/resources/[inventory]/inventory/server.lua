exports('AddItem', function(source, item, count)
    return source, item, count
end)

RegisterNetEvent('inventory:use')
AddEventHandler('inventory:use', function()
    return true
end)
