# Have I paid this twice?

Give it your payments as a simple list and it names the ones that look like
doubles — same payee, same amount, close together in time.

**How to use it**: export your payments as a CSV with three columns —
date (YYYY-MM-DD), payee, amount — then, in a terminal:

    surface/run.sh payments.csv

It prints each suspected double with both dates. It changes nothing,
sends nothing, and works entirely on this machine.

**What it is not**: a verdict. It flags LOOKALIKES for your eye — a
standing order is supposed to repeat; only you (or Aunty Pru, when she
moves in with her proven sums) can say which doubles are real.
