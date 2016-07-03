# web-hawk

Status monitoring tool inspired by BitBucket's status page

![ScreenShot](https://raw.github.com/icha024/web-hawk/master/webHawkScreenshot.png)


#### To Run
1. Start a static web server, eg.
> static-server

2. Start RethinkDB, eg if using Docker:
> docker run --name some-rethink -v "$PWD:/data" -ti -p 8080:8080 -p 28015:28015 rethinkdb

3. Build and run the app, ports and CORS settings are optional.
> go build && ./web-hawk -PORT=3001 -CORS=http://localhost:9080

#### Options
```bash
Usage of ./web-hawk:
  -CORS string
    	CORS URL to configure.
  -DB_ADDDRESS string
    	Address of RethinkDB instance (default "localhost:28015")
  -DB_NAME string
    	Name of RethinkDB database (default "hawk")
  -DB_PASSWORD string
    	Password of RethinkDB user (default "hawkpassw0rd")
  -DB_USERNAME string
    	Username of RethinkDB user (default "web-hawk")
  -POLL_TIME string
    	Time (in seconds) between service status polls. '0' will disable server from polling. (default "600")
  -PORT string
    	Port to host location service on. (default "8080")
  -URLS string
    	Comma seperated URLs list to monitor (default "http://localhost:7070/up, http://www.clianz.com/")
  -URL_CLEANERS string
    	Part of URL to strip for converting to friendly name. (default "http://, https://, www.")
```
### ToDo
- [X] Charts for historic data
- [X] Twitter news integration
- [ ] Notifications
- [ ] Cache and compress server response
- [ ] DB clean up for old data
- [ ] Automate initializing new DB

### Licence
EPL
