local Recipes = {
    sandwich = { ingredients = { bread = 1, water = 1 }, output = 'sandwich', count = 1, duration = 2 },
    field_kit = { ingredients = { medkit = 1, toolkit = 1 }, output = 'field_kit', count = 1, duration = 5 },
    repair_pack = { ingredients = { toolkit = 2, phone = 1 }, output = 'repair_pack', count = 1, duration = 4 },
}

local ActiveCrafts = {}

local function ingredientsAvailable(source, ingredients)
    for item, count in pairs(ingredients or {}) do
        if GetItemCount(source, item) < count then
            return false, item
        end
    end
    return true
end

function RegisterRecipe(name, recipe)
    if not name or not recipe or not recipe.ingredients or not recipe.output then
        return false, 'recipe_invalid'
    end
    Recipes[name] = SharedUtils.copy(recipe)
    return true
end

function GetRecipe(name)
    return Recipes[name]
end

function ListRecipes()
    return SharedUtils.copy(Recipes)
end

function CanCraft(source, name)
    local recipe = Recipes[name]
    if not recipe then
        return false, 'recipe_missing'
    end
    local available, missing = ingredientsAvailable(source, recipe.ingredients)
    if not available then
        return false, 'ingredient_missing:' .. missing
    end
    if not CanCarryItem(source, recipe.output, recipe.count) then
        return false, 'output_cannot_carry'
    end
    return true
end

function StartCraft(source, name)
    local allowed, reason = CanCraft(source, name)
    if not allowed then
        return false, reason
    end
    if ActiveCrafts[source] then
        return false, 'craft_in_progress'
    end
    local recipe = Recipes[name]
    ActiveCrafts[source] = { recipe = name, started = os.time(), duration = recipe.duration }
    TriggerClientEvent('atlas:craftStarted', source, name, recipe.duration)
    return true
end

function FinishCraft(source)
    local active = ActiveCrafts[source]
    if not active then
        return false, 'craft_missing'
    end
    local recipe = Recipes[active.recipe]
    local allowed, reason = CanCraft(source, active.recipe)
    if not allowed then
        ActiveCrafts[source] = nil
        return false, reason
    end
    for item, count in pairs(recipe.ingredients) do
        local removed = RemoveItem(source, item, count)
        if not removed then
            ActiveCrafts[source] = nil
            return false, 'ingredient_changed'
        end
    end
    local added, addError = AddItem(source, recipe.output, recipe.count)
    ActiveCrafts[source] = nil
    if not added then
        return false, addError
    end
    TriggerClientEvent('atlas:craftFinished', source, recipe.output)
    return true
end

function CancelCraft(source)
    if not ActiveCrafts[source] then
        return false
    end
    ActiveCrafts[source] = nil
    TriggerClientEvent('atlas:craftCancelled', source)
    return true
end

RegisterNetEvent('atlas:craftStart')
AddEventHandler('atlas:craftStart', function(name)
    StartCraft(source, name)
end)

RegisterNetEvent('atlas:craftFinish')
AddEventHandler('atlas:craftFinish', function()
    FinishCraft(source)
end)

RegisterNetEvent('atlas:craftCancel')
AddEventHandler('atlas:craftCancel', function()
    CancelCraft(source)
end)

RegisterCommand('craft:list', function(source)
    TriggerClientEvent('atlas:recipeList', source, ListRecipes())
end)

exports('RegisterRecipe', RegisterRecipe)
exports('GetRecipe', GetRecipe)
exports('CanCraft', CanCraft)
exports('StartCraft', StartCraft)
