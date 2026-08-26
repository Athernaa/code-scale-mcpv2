function externalInventory(source)
    return exports.external_inventory:AddItem(source, 'water', 1)
end

function dynamicInventory(source, config)
    return exports[config.resource]:AddItem(source, 'water', 1)
end

function duplicateInventory(source)
    return exports.inventory:AddItem(source, 'water', 1)
end
