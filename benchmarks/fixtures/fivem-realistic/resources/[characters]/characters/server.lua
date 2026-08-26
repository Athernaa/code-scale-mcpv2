local Characters = {}
local ActiveCharacter = {}

local function normalizeCharacter(row)
    return {
        id = row.id,
        name = row.name,
        job = row.job or 'unemployed',
        metadata = row.metadata or {},
        appearance = row.appearance or { model = 'mp_m_freemode_01', version = CharacterConfig.appearanceVersion },
    }
end

local function loadCharacterRows(identifier)
    return Characters[identifier] or {}
end

local function chooseCharacter(rows, characterId)
    if characterId then
        for _, row in ipairs(rows) do
            if row.id == characterId then
                return row
            end
        end
    end
    return rows[1]
end

local function characterForSource(source)
    local identifier = GetPlayerIdentifier(source, 0) or ('source:' .. tostring(source))
    local rows = loadCharacterRows(identifier)
    return identifier, rows
end

function GetCharacter(source)
    local identifier = ActiveCharacter[source]
    if not identifier then
        return nil
    end
    for _, character in ipairs(loadCharacterRows(identifier)) do
        if character.id == ActiveCharacter[source .. ':character'] then
            return character
        end
    end
    return nil
end

function SaveCharacter(source, character)
    local identifier = ActiveCharacter[source]
    if not identifier or not character or not CharacterConfig.validName(character.name) then
        return false, 'invalid_character'
    end
    local rows = loadCharacterRows(identifier)
    for index, row in ipairs(rows) do
        if row.id == character.id then
            rows[index] = normalizeCharacter(character)
            return true
        end
    end
    rows[#rows + 1] = normalizeCharacter(character)
    return true
end

function LoadCharacter(source, characterId)
    local identifier, rows = characterForSource(source)
    local selected = chooseCharacter(rows, characterId)
    if not selected then
        selected = normalizeCharacter({ id = identifier .. ':default', name = 'New Citizen', job = 'unemployed' })
        rows[#rows + 1] = selected
    end
    ActiveCharacter[source] = identifier
    ActiveCharacter[source .. ':character'] = selected.id
    local player = exports.core:RegisterSession(source, identifier, selected)
    local carried, carryError = exports.inventory:AddItem(source, 'water', 1)
    if not carried then
        TriggerClientEvent('atlas:characterLoadWarning', source, carryError)
    end
    TriggerClientEvent('atlas:characterLoaded', source, selected, player)
    return selected
end

function UnloadCharacter(source)
    exports.inventory:SaveInventory(source)
    exports.core:SavePlayer(source)
    TriggerEvent('atlas:playerLogout', source)
    ActiveCharacter[source] = nil
    ActiveCharacter[source .. ':character'] = nil
    return true
end

RegisterNetEvent('atlas:characterLoad')
AddEventHandler('atlas:characterLoad', function(characterId)
    LoadCharacter(source, characterId)
end)

RegisterNetEvent('atlas:characterUnload')
AddEventHandler('atlas:characterUnload', function()
    UnloadCharacter(source)
end)

RegisterCommand('character:load', function(source, _, args)
    LoadCharacter(source, args[1])
end)

exports('LoadCharacter', LoadCharacter)
exports('GetCharacter', GetCharacter)
exports('SaveCharacter', SaveCharacter)
exports('UnloadCharacter', UnloadCharacter)
