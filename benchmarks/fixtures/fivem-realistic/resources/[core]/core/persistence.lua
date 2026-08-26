local Persistence = {}
local Pending = {}
local Writes = {}

local function keyFor(identifier, section)
    return tostring(identifier) .. ':' .. tostring(section)
end

local function ensurePending(identifier, section)
    local key = keyFor(identifier, section)
    if not Pending[key] then
        Pending[key] = { identifier = identifier, section = section, values = {}, dirty = false, version = 0 }
    end
    return Pending[key]
end

function Persistence.read(identifier, section)
    local value = ensurePending(identifier, section)
    return SharedUtils.copy(value.values), value.version
end

function Persistence.write(identifier, section, values)
    local pending = ensurePending(identifier, section)
    pending.values = SharedUtils.copy(values or {})
    pending.dirty = true
    pending.version = pending.version + 1
    return true, pending.version
end

function Persistence.patch(identifier, section, values)
    local pending = ensurePending(identifier, section)
    pending.values = SharedUtils.merge(pending.values, values)
    pending.dirty = true
    pending.version = pending.version + 1
    return true, pending.version
end

function Persistence.delete(identifier, section)
    local key = keyFor(identifier, section)
    Pending[key] = nil
    return true
end

function Persistence.flush(identifier, section)
    local pending = ensurePending(identifier, section)
    if not pending.dirty then
        return false, 'clean'
    end
    local write = { identifier = identifier, section = section, version = pending.version, values = SharedUtils.copy(pending.values), at = os.time() }
    Writes[#Writes + 1] = write
    pending.dirty = false
    return true, write
end

function Persistence.flushAll(identifier)
    local flushed = 0
    for key, pending in pairs(Pending) do
        if not identifier or pending.identifier == identifier then
            local ok = Persistence.flush(pending.identifier, pending.section)
            if ok then
                flushed = flushed + 1
            end
        end
    end
    return flushed
end

function Persistence.isDirty(identifier, section)
    return ensurePending(identifier, section).dirty
end

function Persistence.history(identifier, section)
    local result = {}
    for _, write in ipairs(Writes) do
        if write.identifier == identifier and write.section == section then
            result[#result + 1] = write
        end
    end
    return result
end

function Persistence.transaction(identifier, changes)
    local originals = {}
    for _, change in ipairs(changes or {}) do
        local current = Persistence.read(identifier, change.section)
        originals[change.section] = current
        Persistence.patch(identifier, change.section, change.values)
    end
    local ok, errorMessage = true, nil
    for _, change in ipairs(changes or {}) do
        local flushed, value = Persistence.flush(identifier, change.section)
        if not flushed and value ~= 'clean' then
            ok, errorMessage = false, value
            break
        end
    end
    if not ok then
        for section, values in pairs(originals) do
            Persistence.write(identifier, section, values)
        end
        return false, errorMessage
    end
    return true
end

RegisterNetEvent('atlas:persistenceFlush')
AddEventHandler('atlas:persistenceFlush', function()
    local player = GetPlayer(source)
    if player then
        Persistence.flushAll(player.identifier)
    end
end)

exports('PersistenceRead', Persistence.read)
exports('PersistenceWrite', Persistence.write)
exports('PersistencePatch', Persistence.patch)
exports('PersistenceFlush', Persistence.flush)
exports('PersistenceTransaction', Persistence.transaction)

return Persistence
