local Repository = {}
local Records = {}

local function identifierFor(source)
    return GetPlayerIdentifier(source, 0) or ('source:' .. tostring(source))
end

local function ensureRows(identifier)
    if not Records[identifier] then
        Records[identifier] = {}
    end
    return Records[identifier]
end

function Repository.list(source)
    return SharedUtils.copy(ensureRows(identifierFor(source)))
end

function Repository.find(source, characterId)
    for _, character in ipairs(ensureRows(identifierFor(source))) do
        if character.id == characterId then
            return SharedUtils.copy(character)
        end
    end
    return nil
end

function Repository.insert(source, character)
    if not character or not CharacterConfig.validName(character.name) then
        return false, 'invalid_character'
    end
    local identifier = identifierFor(source)
    local rows = ensureRows(identifier)
    local record = SharedUtils.copy(character)
    record.id = record.id or SharedUtils.newId(identifier, #rows + 1)
    rows[#rows + 1] = record
    return true, SharedUtils.copy(record)
end

function Repository.update(source, characterId, patch)
    local rows = ensureRows(identifierFor(source))
    for index, character in ipairs(rows) do
        if character.id == characterId then
            rows[index] = SharedUtils.merge(character, patch)
            return true, SharedUtils.copy(rows[index])
        end
    end
    return false, 'character_missing'
end

function Repository.delete(source, characterId)
    local rows = ensureRows(identifierFor(source))
    for index, character in ipairs(rows) do
        if character.id == characterId then
            table.remove(rows, index)
            return true
        end
    end
    return false, 'character_missing'
end

function Repository.count(source)
    return #ensureRows(identifierFor(source))
end

function Repository.validate(source)
    local errors = {}
    for _, character in ipairs(ensureRows(identifierFor(source))) do
        if not character.id then
            errors[#errors + 1] = 'id_missing'
        end
        if not CharacterConfig.validName(character.name) then
            errors[#errors + 1] = 'name_invalid'
        end
    end
    return #errors == 0, errors
end

function Repository.seed(source, values)
    local rows = ensureRows(identifierFor(source))
    for _, value in ipairs(values or {}) do
        local ok, record = Repository.insert(source, value)
        if not ok then
            return false, record
        end
        rows[#rows] = record
    end
    return true
end

RegisterNetEvent('atlas:characterList')
AddEventHandler('atlas:characterList', function()
    TriggerClientEvent('atlas:characterListResult', source, Repository.list(source))
end)

exports('ListCharacters', Repository.list)
exports('FindCharacter', Repository.find)
exports('UpdateCharacter', Repository.update)

return Repository
