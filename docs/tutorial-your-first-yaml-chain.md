# Your First YAML Chain

In this tutorial we'll write a chain in a YAML file, load it, and watch **pg_timetable** run it on startup.

1. Make sure **pg_timetable** is installed and you have a role with `CREATE` privilege on a
   target database — see the [first tutorial](tutorial-first-chain.md) if you haven't done that yet.

2. Create a file named `hello.yaml`:

    ```yaml
    chains:
      - name: "hello-yaml"
        schedule: "@reboot"
        live: true

        tasks:
          - name: "hello"
            kind: "BUILTIN"
            command: "Log"
            parameters:
              - "Hello from my first YAML chain!"
    ```

3. Validate the file before loading it:

    ```bash
    # pg_timetable --file hello.yaml --validate postgresql://scheduler:somestrong@localhost/my_database
    ```

    You'll see:

    ```text
    YAML file validation successful
    ```

4. Load the chain and let **pg_timetable** run it. `@reboot` chains execute as soon as the
   scheduler starts, so you'll see the result immediately:

    ```bash
    # pg_timetable --file hello.yaml --clientname=yamltester postgresql://scheduler:somestrong@localhost/my_database
    ```

5. In a second terminal, check what the chain produced:

    ```sql
    my_database=> SELECT output FROM timetable.execution_log
    my_database-> WHERE chain_id = (SELECT chain_id FROM timetable.chain WHERE chain_name = 'hello-yaml');
                    output
    ------------------------------------------
     Logged: Hello from my first YAML chain!
    (1 row)
    ```

6. PROFIT!

For the full field list, task kinds, and error-handling options, see the
[YAML Chain Configuration Guide](how-to-write-yaml-chains.md) and the
[YAML Chain Schema](yaml-format.md) reference.
