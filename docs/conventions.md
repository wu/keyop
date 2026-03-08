# Conventions

Currently this list is just a parking lot for some random thoughts. It will be improved and organized over time.

* package names should be generic, e.g. 'speak', not implementation specific
* event names should be prefixed with package names, e.g. 'speak_error'
* services should register event payloads

* use 'summary' field for short descriptions that might be spoken by voice assistants
* use 'text' field for longer descriptions that might be displayed on screens
* don't set 'summary' without setting 'text'

