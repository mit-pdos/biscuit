#include <litc.h>

void *fn(void *a)
{
	printf("happy day\n");
	return NULL;
}

int main(int argc, char **argv)
{
	pthread_t t;

	printf("make threads\n");
	if (pthread_create(&t, NULL, fn, NULL))
		errx(-1, "pthread create");
	printf("started\n");
	void *ret;
	if (pthread_join(t, &ret))
		errx(-1, "pthread join");
	printf("joined\n");
	return 0;
}
