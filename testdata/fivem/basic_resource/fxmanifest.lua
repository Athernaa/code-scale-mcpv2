fx_version 'cerulean'
game 'gta5'

client_scripts {
    'client/*.lua'
}

server_script 'server/main.lua'

shared_scripts {
    '@ox_lib/init.lua',
    'shared/*.lua'
}

dependency 'ox_lib'

dependencies {
    'qbx_core',
    'ox_inventory'
}

ui_page 'web/build/index.html'
