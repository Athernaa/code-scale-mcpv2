function dynamicResource(source, config)
    return exports[config.resource]:AddItem(source, 'water', 1)
end
