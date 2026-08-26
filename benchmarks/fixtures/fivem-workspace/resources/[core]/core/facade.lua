function GetPlayer(source)
    return source
end

function GetCoreObject(source)
    return { Functions = { GetPlayer = GetPlayer(source) } }
end
