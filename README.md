# Freckle project indicators

This repository contains a simple Go application that extract information from Freckle's API.
This utility give you a global view as well per month summary.

In the overall aggregate you will find :

* Total amount invoiced
* Number of hours invoiced
* Invoiced hourly rate
* Number of hours billable
* Billable hourly rate
* Number of hours unbillable

The Per Month summary gives you :

* Amount invoiced during this period
* Number of billable hours for each participants
* Number of unbillable hours for each participants

## Installation


```
go get github.com/yml/freckle-project-indicators
```


## Usage

In order to use this application you need to set your Freckle API Token as environment variable.

```
FRECKLE_APP_TOKEN="<API TOKEN GOES HERE>" freckle-project-indicators "noodle.com"
```

You can restrict the report to a list a project by passing them as arguments. If no project are specified the report will extract information for all of them.
