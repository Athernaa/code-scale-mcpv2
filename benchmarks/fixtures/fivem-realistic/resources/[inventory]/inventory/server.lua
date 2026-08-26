local Inventories = {}
local ItemDefinitions = {
    water = { label = 'Water', usable = true },
    bread = { label = 'Bread', usable = true },
    medkit = { label = 'Medical Kit', usable = true },
    toolkit = { label = 'Toolkit', usable = true },
    phone = { label = 'Phone', usable = true },
}

local function emptyInventory()
    return { slots = {}, weight = 0, dirty = false, version = 0 }
end

local function getInventory(source)
    if not Inventories[source] then
        Inventories[source] = emptyInventory()
    end
    return Inventories[source]
end

local function findSlot(inventory, item)
    for index, slot in pairs(inventory.slots) do
        if slot.item == item then
            return index, slot
        end
    end
    return nil, nil
end

local function findFreeSlot(inventory)
    for index = 1, InventoryConfig.slots do
        if not inventory.slots[index] then
            return index
        end
    end
    return nil
end

local function validateCount(count)
    return type(count) == 'number' and count > 0 and count == math.floor(count)
end

function GetInventory(source)
    return getInventory(source)
end

function GetItemCount(source, item)
    local inventory = getInventory(source)
    local _, slot = findSlot(inventory, item)
    return slot and slot.count or 0
end

function CanCarryItem(source, item, count)
    if not InventoryConfig.isValidItem(item) or not validateCount(count) then
        return false
    end
    local inventory = getInventory(source)
    return inventory.weight + InventoryConfig.weightOf(item, count) <= InventoryConfig.maxWeight
end

function AddItem(source, item, count, metadata)
    if not CanCarryItem(source, item, count) then
        return false, 'cannot_carry'
    end
    local inventory = getInventory(source)
    local index, slot = findSlot(inventory, item)
    if not slot then
        index = findFreeSlot(inventory)
        if not index then
            return false, 'inventory_full'
        end
        slot = { item = item, count = 0, metadata = metadata or {} }
        inventory.slots[index] = slot
    end
    slot.count = slot.count + count
    inventory.weight = inventory.weight + InventoryConfig.weightOf(item, count)
    inventory.version = inventory.version + 1
    inventory.dirty = true
    TriggerClientEvent('atlas:inventoryChanged', source, inventory.version)
    return true, slot
end

function RemoveItem(source, item, count)
    if not InventoryConfig.isValidItem(item) or not validateCount(count) then
        return false, 'invalid_item'
    end
    local inventory = getInventory(source)
    local index, slot = findSlot(inventory, item)
    if not slot or slot.count < count then
        return false, 'not_enough_items'
    end
    slot.count = slot.count - count
    inventory.weight = inventory.weight - InventoryConfig.weightOf(item, count)
    if slot.count == 0 then
        inventory.slots[index] = nil
    end
    inventory.version = inventory.version + 1
    inventory.dirty = true
    TriggerClientEvent('atlas:inventoryChanged', source, inventory.version)
    return true
end

function MoveItem(source, fromIndex, toIndex)
    local inventory = getInventory(source)
    if not inventory.slots[fromIndex] or toIndex < 1 or toIndex > InventoryConfig.slots then
        return false, 'invalid_slot'
    end
    inventory.slots[fromIndex], inventory.slots[toIndex] = inventory.slots[toIndex], inventory.slots[fromIndex]
    inventory.version = inventory.version + 1
    inventory.dirty = true
    return true
end

function ClearInventory(source)
    Inventories[source] = emptyInventory()
    TriggerClientEvent('atlas:inventoryChanged', source, 0)
    return true
end

function SaveInventory(source)
    local inventory = Inventories[source]
    if not inventory then
        return false, 'inventory_missing'
    end
    inventory.dirty = false
    return true
end

function UseItem(source, item)
    local definition = ItemDefinitions[item]
    if not definition or not definition.usable or GetItemCount(source, item) < 1 then
        return false, 'item_unavailable'
    end
    TriggerEvent('atlas:itemUsed', source, item)
    return true
end

RegisterNetEvent('atlas:inventoryRequest')
AddEventHandler('atlas:inventoryRequest', function()
    TriggerClientEvent('atlas:inventorySnapshot', source, GetInventory(source))
end)

RegisterNetEvent('atlas:inventorySave')
AddEventHandler('atlas:inventorySave', function()
    SaveInventory(source)
end)

RegisterCommand('inventory:clear', function(source)
    ClearInventory(source)
end)

exports('GetInventory', GetInventory)
exports('GetItemCount', GetItemCount)
exports('CanCarryItem', CanCarryItem)
exports('AddItem', AddItem)
exports('RemoveItem', RemoveItem)
exports('MoveItem', MoveItem)
exports('SaveInventory', SaveInventory)
