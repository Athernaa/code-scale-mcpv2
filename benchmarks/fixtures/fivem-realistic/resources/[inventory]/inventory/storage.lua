local Storage = {}
local Containers = {}

local function containerKey(owner, name)
    return tostring(owner) .. ':' .. tostring(name)
end

local function ensureContainer(owner, name)
    local key = containerKey(owner, name)
    if not Containers[key] then
        Containers[key] = { owner = owner, name = name, slots = {}, weight = 0, capacity = 200, version = 0 }
    end
    return Containers[key]
end

function Storage.open(owner, name, capacity)
    local container = ensureContainer(owner, name)
    if capacity then
        container.capacity = capacity
    end
    container.version = container.version + 1
    return container
end

function Storage.close(owner, name)
    local key = containerKey(owner, name)
    local container = Containers[key]
    if not container then
        return false
    end
    container.version = container.version + 1
    return true
end

function Storage.get(owner, name)
    return Containers[containerKey(owner, name)]
end

function Storage.hasSpace(owner, name, item, count)
    local container = ensureContainer(owner, name)
    return container.weight + InventoryConfig.weightOf(item, count) <= container.capacity
end

function Storage.add(owner, name, item, count, metadata)
    if not Storage.hasSpace(owner, name, item, count) then
        return false, 'storage_full'
    end
    local container = ensureContainer(owner, name)
    local slot
    for _, value in pairs(container.slots) do
        if value.item == item then
            slot = value
            break
        end
    end
    if not slot then
        local index = 1
        while container.slots[index] do
            index = index + 1
        end
        slot = { item = item, count = 0, metadata = metadata or {} }
        container.slots[index] = slot
    end
    slot.count = slot.count + count
    container.weight = container.weight + InventoryConfig.weightOf(item, count)
    container.version = container.version + 1
    return true, container
end

function Storage.remove(owner, name, item, count)
    local container = Storage.get(owner, name)
    if not container then
        return false, 'storage_missing'
    end
    for index, slot in pairs(container.slots) do
        if slot.item == item then
            if slot.count < count then
                return false, 'not_enough_items'
            end
            slot.count = slot.count - count
            container.weight = container.weight - InventoryConfig.weightOf(item, count)
            if slot.count == 0 then
                container.slots[index] = nil
            end
            container.version = container.version + 1
            return true
        end
    end
    return false, 'item_missing'
end

function Storage.transfer(source, fromName, toName, item, count)
    local owner = GetPlayer(source) and GetPlayer(source).identifier or source
    if not Storage.hasSpace(owner, toName, item, count) then
        return false, 'target_full'
    end
    local removed, removeError = Storage.remove(owner, fromName, item, count)
    if not removed then
        return false, removeError
    end
    local added, addError = Storage.add(owner, toName, item, count)
    if not added then
        Storage.add(owner, fromName, item, count)
        return false, addError
    end
    return true
end

function Storage.clear(owner, name)
    local container = ensureContainer(owner, name)
    container.slots = {}
    container.weight = 0
    container.version = container.version + 1
    return true
end

function Storage.snapshot(owner, name)
    local container = Storage.get(owner, name)
    if not container then
        return nil
    end
    return SharedUtils.copy(container)
end

return Storage
