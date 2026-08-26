InventoryConfig = {
    maxWeight = 120,
    slots = 40,
    defaultItem = 'water',
    itemWeights = { water = 1, bread = 1, medkit = 2, toolkit = 5, phone = 1 },
}

function InventoryConfig.weightOf(item, count)
    return (InventoryConfig.itemWeights[item] or 1) * (count or 1)
end

function InventoryConfig.isValidItem(item)
    return type(item) == 'string' and item ~= '' and InventoryConfig.itemWeights[item] ~= nil
end
