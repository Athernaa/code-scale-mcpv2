exports('AddItem', function(source, item, count)
    return source, item, count
end)

exports('RemoveItem', function(source, item, count)
    return source, item, count
end)

RegisterNetEvent('inventory:use')
AddEventHandler('inventory:use', function(item)
    return item
end)

lib.callback.register('inventory:get', function(source)
    return source
end)
