if (process.argv.length != 3) {
    console.error("Initial password for Admin must be specified")
    process.exit(1)
}

r = require('rethinkdb')
r.connect({ host: 'localhost', port: 28015 }, function (err, conn) {
    if (err) throw err;
    r.db('test').tableCreate('hawk').run(conn, function (err, res) {
        if (err) throw err;
        console.log(res);
        r.db('rethinkdb').table('users').insert({ id: 'web-hawk', password: 'hawkpassw0rd' }).run(conn, function (err, res) {
            if (err) throw err;
            console.log(res);
            r.db('test').table('hawk').grant('web-hawk', { read: true, write: true, config: false }).run(conn, function (err, res) {
                if (err) throw err;
                console.log(res);
                r.db('rethinkdb').table('users').get('admin').update({password: process.argv[2]}).run(conn, function (err, res) {
                    if (err) throw err;
                    console.log(res);
                    console.log("Successfully created new table 'hawk' with user 'web-hawk'")
                    console.log("Admin password updated.")
                    process.exit(0)
                });
            });
        });
    });
});
