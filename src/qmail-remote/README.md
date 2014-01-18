#qmail-remote

## Appel à contribution
Par manque de temps je vais rédiger la doc en Français, si quelqu'un se sent de la traduire en Anglais, grand bien lui prenne ;)

## Features (vs qmail-remote vanilla) 
* SMTPAUTH
* TLS
* Routes en fonction de l'expéditeur et/ou du destinataire.
* Utilisation d'une ou plusieurs IP locale(s), en fonction de l'expéditeur et/ou du destinaire.
* Failover ou Round Robin, sur les routes.
* Failover ou Round Robin sur les IP locales.

### SMTP AUTH
SMTPAUTH permet de s'authentifier auprés des relais vers lesquel le serveur doit transmettre les mails.
Les méthodes PLAIN et CRAM-MD5 sont implémentées. 

### TLS
TLS permet de chiffrer la transaction entre votre serveur et le serveur suivant si ce denier le supporte.

**ATTENTION** : Il y a un probléme avec certain serveurs qui fait que TLS ne va pas fonctionner même si le serveur d'en face le supporte. Dans ce cas, plutot que de générer une erreur, j'ai préféré continuer avec une transaction non chiffrée. Gardez bien ça en tête.

### Routes
Vous allez pouvoir définir des routes en fonction du domaine de l'expéditeur, ou du domaine du destinataire ou des deux. C'est une amélioration du systéme par défaut (smtproutes)

### IP locales
Il vous sera possible de définir plusieurs IP locales. Votre serveur sera vu du relais suivant comme ayant l'IP selectionnée. Ca peut etre utile si par exemple vous souhaitez attribuer une IP a un client, ou eclater les IP en fonction des destinations, ou... 

### Fail Over et Round Robin
Vous pouvez appliquer des régles de failover ou de round robin sur les routes et les IP locales.
Un exemple typique d'usage pour les routes et d'utiliser la route pas défaut (celle donnée par les MX) en priorité et de une serconde vers un serveur que vous avez reservé pour les relais difficiles à joindre. Si la premiére ne passe pas alors le mail sera redirigé sur la seconde et ainsi la queue du serveur principal ne sera pas engorgé inutilement.
Concernant les IP locales on peut par exemple utiliser le round robin pour "diluer" les envois sur plusieurs IP.


## Installation

1 - Stopez qmail

2 - Faites un copie de votre qmail-remote original

	cp /var/qmail/bin/qmail-remote /var/qmail/bin/qmail-remote.ori
	
3 - Ajoutez les fichiers de config suivant (je reviendrais plus tard dessus)

Le fichier contenant votre IP locale à utiliser par défaut :

	touch /var/qmail/control/defaultoutgoingip
Le fichier de routes:
	
	touch /var/qmail/control/routes
Le fichier qui lie les expéditeurs/destinataires aux routes :

	touch /var/qmail/control/routemap

4 - Récuperez la derniere version compilée (vous pouvez compilez vous même les sources de ce repo mais attention je ne garantis pas que le binaire obtenu soit stable, je fais des push en cours de dev... )

	wget http://dl.toorop.fr/softs/qmail-booster/qmail-remote -O /var/qmail/bin/qmail-remote

	
## Configuration		

### defaultoutgoingip
Mettez simplement dans ce fichier l'IP sortante que vous souhaitez utiliser par defaut.

Attention même si vous avez une seule IP il est indispensable de renseigner ce fichier.

#### routes
Ce fichier va définir les differentes routes.

Chaque ligne va représenter une route.
(une ligne commençant par # va etre considérée comme un commentaire)

Le format d'un ligne est le suivant :

	NAME;LOCAL_ADDRESSE(S);REMOTE_ADDRESSE(S);USERNAME;PASSWD

Avec :
	
* NAME :  Le nom de la route. Obligatoire. AlphaNumérique sans espace
 
* LOCAL_ADDRESSE(S): La liste des adresses IP locales à utiliser. 
Cette liste peu avoir 0, 1 ou plusieurs IP. Si elle n'est pas renseignée ce sera la valeur de "defaultoutgoingip" qui sera utilisée. Les séparateurs à utiliser sont "&" si vous souahitez faire du failover ou "|" pour du roud robin. Attention vous ne pouvez mixer "&" et "|s" dans une même liste. (voir exemples).

* REMOTE_ADDRESSE(S) : La liste des serveurs à joindre pour transmettre le mail. Peut contenir 0, 1 ou plusieurs addresses. Si elle n'est pas renseignée ce sera une requete DNS MX sera faite pour le domaine concerné. Chaque adresse à le format suivant IP:PORT ou HOSTAME:PORT. A la place d'une adresse vous pouvez mettre "mx", dans ce cas ce sont les MX du domaine de destination qui seront utilisés (**uniquement dans si il est utilisé seul ou avec d'autre destinations mais uniquement en failover, si vous utiliser "mx" en round robin vous allez avoir une erreur**) Là aussi les séparateurs sont soit "&" pour du failover soit "|" pour du round robin. Attention vous ne pouvez mixer "&" et "|" dans une même liste. (voir exemples)

* USERNAME : si le serveur suivant nécéssite une authentification, vous devez définir le nom d'utilisateur ici.

* PASSWD : idem pour le mot de passe.


#### Exemples
	
	route1;;;;
Cette route ne fait rien de spéciale, elle va utilise l'IP locale et va transmettre le mail au MX renseignés dans les enregistrement DNS du domaine de destination.

	route2;1.1.1.1;2.2.2.2:465;admin;123456
Les mails qui vont utiliser cette route vont sortir par l'IP locale 1.1.1.1 à destination de 2.2.2.2 sur le port 465. Une authentification sera faite auprés de 2.2.2.2.	
	
	route3;1.1.1.1;mx&3.3.3.3:587;;
	
Les mails qui vont emprunter cette route vont sortir par l'IP 1.1.1.1 a destination des IP correspondant à l'enregistrement MX du domaine de destination. Si aucune des IP des MX ne répond alors le mail sera transmis a 3.3.3.3 sur le port 587.

	route4;1.1.1.1|2.2.2.2;;;
Les mails qui vont transitez par cette route vont avoir comme IP sortantes 1.1.1.1 ou 2.2.2.2 et vont etre transmis aux MX enregistrés dans les DNS.	

Vous êtes toujours là ? OK on va compliquer un peu.
Imaginons que vous ayez un hebergeur qui a décidé un beau matin de filtrer les mails qui sortent de vos serveur et qui si ils ne lui plaisent pas se donne le droit de bloquer toute sortie de votre IP vers le port 25 d'une autre IP. 

Oui je sais ça demande pas mal d'imagination, mais bon... ;-°

	route5;1.1.1.1&2.2.2.2&3.3.3.3&4.4.4.4;mx&6.6.6.6:587;;

Avec cette route on va d'abord tenter de sortir par 1.1.1.1 vers les MX, puis si 1.1.1.1 est bloquée on va essayer avec l'IP locale 2.2.2.2, puis avec l'IP locale 3.3.3.3, puis 4.4.4.4, si ça ne passe toujours pas autrement dit si notre hébergeur à bloqué toutes les sorties vers un port 25 distant et ce pour toutes les IP alors les mails vont sortir en utilisant l'IP locale 1.1.1.1 à destination de 6.6.6.6:587

### routemap
Ce dernier fichier de config sert à associer chaque mail à une route.

Son format est le suivant :
	
	EXPEDITEUR;DESTINATAIRE;ROUTE
	
Avec :

* EXPEDITEUR : le domaine de l'expédideur. "*" est une wildcard qui veux dire tous les domaines.

* DESTINATAIRE : le domaine du destinataire. "*" est ici aussi une wildcard qui signifie tous les domaines de destination.

* ROUTE : le nom de la route à utiliser tel que definie dans le fichier routes.

A noter que les routes sont testée de la première à la derniére et la première qui matche est utilisée.

Exemples :
	
	domaine1.com;*;route1
	
-> tous les mails expédiés depuis le domaine "domaine1.com" doivent utiliser la route "route1".


	*;domaine2.com;route2
-> Tous les mails a destination de "domaine2.com" doivent utiliser la route "route2".		

	domaine1.com;domaine2.com;route3	
-> Tous les mails du domaine "domaine1.com" et à destination du domaine "domaine2.com" doivent utiliser la route "route3".

	*;*;route4
-> Tous les mails qui ne matchent pas une des routes précédente utiliseront la route "route4"	

		

	
 
	



	
 