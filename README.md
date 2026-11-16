This is the server-side code for the Beatblock Multiplayer mod.\

## Hosting your own lobby
This is for advanced users, if you simply want to play with your friend consider using the official multiplayer server instead.

You need:
- A machine to run the server on, with 1 port open to the internet. `25656` is the default port used in the client.
- A calculator CPU
- 10MB of RAM
- Internet fast enough to download a WAV file in 2 weeks

Port forwarding process is different depending on your OS, ISP, router ... It won't be included here

- Run `build_win` or `build_linux` to compile. The server executable will be inside `build/` or download it from [releases](https://github.com/erenkarakal/BeatblockMultiplayerServer/releases/latest)
- Simply run the server.

### Startup Parameters
| flag               | default  | meaning                                                                                            |
|--------------------|----------|----------------------------------------------------------------------------------------------------|
| `-listen`          | `:25656` | TCP address to bind, `:25656` = all interfaces. `0.0.0.0:port` and `127.0.0.1:port` are also valid |
| `-max-players`     | `32`     | Per lobby player cap                                                                               |
| `-max-connections` | `128`    | Player limit across all lobbies                                                                    |
| `-round-timeout`   | `15m`    | When it expires, all remaining players are marked as DNF, will be reworked in the future           |
| `-verbose`         | `false`  | Enable verbose logging                                                                             |
