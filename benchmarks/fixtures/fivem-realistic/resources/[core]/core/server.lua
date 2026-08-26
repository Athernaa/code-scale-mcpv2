local Players = {}
local Accounts = {}
local Sessions = {}

local function defaultAccount(identifier)
    return { cash = CoreConfig.defaultCash, bank = CoreConfig.defaultBank, dirty = true }
end

local function ensureAccount(identifier)
    if not Accounts[identifier] then
        Accounts[identifier] = defaultAccount(identifier)
    end
    return Accounts[identifier]
end

local function buildPlayer(source, identifier, character)
    local account = ensureAccount(identifier)
    return {
        source = source,
        identifier = identifier,
        characterId = character.id,
        name = character.name,
        job = character.job,
        account = account,
        metadata = character.metadata or {},
    }
end

function GetPlayer(source)
    return Players[source]
end

function GetPlayerByIdentifier(identifier)
    for _, player in pairs(Players) do
        if player.identifier == identifier then
            return player
        end
    end
    return nil
end

function RegisterSession(source, identifier, character)
    local player = buildPlayer(source, identifier, character)
    Players[source] = player
    Sessions[identifier] = source
    return player
end

function RemoveSession(source)
    local player = Players[source]
    if not player then
        return false
    end
    Players[source] = nil
    if Sessions[player.identifier] == source then
        Sessions[player.identifier] = nil
    end
    return true
end

function UpdateJob(source, job, grade)
    local player = Players[source]
    if not player or not CoreConfig.hasJob(job) then
        return false
    end
    player.job = { name = job, grade = grade or 0 }
    return true
end

function AddCash(source, amount)
    local player = Players[source]
    if not player then
        return false
    end
    player.account.cash = player.account.cash + amount
    player.account.dirty = true
    return true
end

function RemoveCash(source, amount)
    local player = Players[source]
    if not player or player.account.cash < amount then
        return false
    end
    player.account.cash = player.account.cash - amount
    player.account.dirty = true
    return true
end

function SavePlayer(source)
    local player = Players[source]
    if not player then
        return false, 'player_missing'
    end
    player.account.dirty = false
    return true
end

function ExportPlayerSnapshot(source)
    local player = Players[source]
    if not player then
        return nil
    end
    return {
        identifier = player.identifier,
        characterId = player.characterId,
        name = player.name,
        job = player.job,
        account = player.account,
    }
end

RegisterNetEvent('atlas:playerLogout')
AddEventHandler('atlas:playerLogout', function()
    RemoveSession(source)
end)

RegisterCommand('atlas:save', function(source)
    SavePlayer(source)
end)

exports('GetPlayer', GetPlayer)
exports('GetPlayerByIdentifier', GetPlayerByIdentifier)
exports('RegisterSession', RegisterSession)
exports('UpdateJob', UpdateJob)
exports('SavePlayer', SavePlayer)
